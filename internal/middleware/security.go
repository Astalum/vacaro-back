package middleware

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	csrfProofHeader = "X-Vacaro-CSRF"
	csrfProofValue  = "same-origin"
)

// NewCSRFProtection rejects unsafe cross-origin browser requests and requires
// an application-specific header that regular cross-origin HTML forms cannot
// send. Non-browser clients must send X-Vacaro-CSRF: same-origin as an explicit
// acknowledgement for unsafe API requests.
func NewCSRFProtection(trustedOrigins []string) (func(http.Handler) http.Handler, error) {
	protection := http.NewCrossOriginProtection()
	for _, origin := range trustedOrigins {
		if err := protection.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("add trusted origin %q: %w", origin, err)
		}
	}
	protection.SetDenyHandler(http.HandlerFunc(writeCSRFDenied))

	return func(next http.Handler) http.Handler {
		requireProof := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isAPIPath(r.URL.Path) && !isSafeMethod(r.Method) && r.Header.Get(csrfProofHeader) != csrfProofValue {
				writeCSRFDenied(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
		return protection.Handler(requireProof)
	}, nil
}

func writeCSRFDenied(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeError(w, http.StatusForbidden, "csrf_validation_failed", "リクエストの送信元を確認できませんでした。ページを再読み込みしてください", RequestIDFromContext(r.Context()))
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func isAPIPath(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

type clientBucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

type ipRateLimiter struct {
	mu              sync.Mutex
	limit           float64
	refillPerSecond float64
	window          time.Duration
	buckets         map[string]*clientBucket
	lastCleanup     time.Time
	trustedProxies  []*net.IPNet
}

// NewRateLimit creates a per-client-IP token-bucket limiter for /api/v1.
// Forwarded client IP headers are used only when the direct peer belongs to a
// configured trusted proxy CIDR.
func NewRateLimit(requests int, window time.Duration, trustedProxyCIDRs []string) (func(http.Handler) http.Handler, error) {
	if requests <= 0 {
		return nil, fmt.Errorf("requests must be positive")
	}
	if window <= 0 {
		return nil, fmt.Errorf("window must be positive")
	}
	trustedProxies := make([]*net.IPNet, 0, len(trustedProxyCIDRs))
	for _, cidr := range trustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy CIDR %q: %w", cidr, err)
		}
		trustedProxies = append(trustedProxies, network)
	}
	limiter := &ipRateLimiter{
		limit:           float64(requests),
		refillPerSecond: float64(requests) / window.Seconds(),
		window:          window,
		buckets:         make(map[string]*clientBucket),
		trustedProxies:  trustedProxies,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAPIPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			allowed, retryAfter := limiter.allow(limiter.clientIP(r), time.Now())
			if !allowed {
				retryAfterSeconds := int(math.Ceil(retryAfter.Seconds()))
				if retryAfterSeconds < 1 {
					retryAfterSeconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
				w.Header().Set("Cache-Control", "no-store")
				writeError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "リクエストが多すぎます。しばらく待ってから再試行してください", RequestIDFromContext(r.Context()))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func (l *ipRateLimiter) allow(client string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= l.window {
		staleBefore := now.Add(-2 * l.window)
		for key, bucket := range l.buckets {
			if bucket.lastSeen.Before(staleBefore) {
				delete(l.buckets, key)
			}
		}
		l.lastCleanup = now
	}

	bucket, exists := l.buckets[client]
	if !exists {
		bucket = &clientBucket{tokens: l.limit, updated: now}
		l.buckets[client] = bucket
	} else if elapsed := now.Sub(bucket.updated).Seconds(); elapsed > 0 {
		bucket.tokens = math.Min(l.limit, bucket.tokens+elapsed*l.refillPerSecond)
		bucket.updated = now
	}
	bucket.lastSeen = now

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}
	retrySeconds := (1 - bucket.tokens) / l.refillPerSecond
	return false, time.Duration(retrySeconds * float64(time.Second))
}

func (l *ipRateLimiter) clientIP(r *http.Request) string {
	remoteIP := parseRemoteIP(r.RemoteAddr)
	if remoteIP == nil {
		return "unknown"
	}
	if !l.isTrustedProxy(remoteIP) {
		return remoteIP.String()
	}

	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		forwardedIPs := make([]net.IP, 0, len(parts))
		for _, part := range parts {
			ip := net.ParseIP(strings.TrimSpace(part))
			if ip == nil {
				return remoteIP.String()
			}
			forwardedIPs = append(forwardedIPs, ip)
		}
		for i := len(forwardedIPs) - 1; i >= 0; i-- {
			if !l.isTrustedProxy(forwardedIPs[i]) {
				return forwardedIPs[i].String()
			}
		}
		if len(forwardedIPs) > 0 {
			return forwardedIPs[0].String()
		}
	}
	if realIP := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); realIP != nil {
		return realIP.String()
	}
	return remoteIP.String()
}

func (l *ipRateLimiter) isTrustedProxy(ip net.IP) bool {
	for _, network := range l.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func parseRemoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = strings.Trim(remoteAddr, "[]")
	}
	return net.ParseIP(host)
}
