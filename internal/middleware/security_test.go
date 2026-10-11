package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestCSRFProtection(t *testing.T) {
	middleware, err := NewCSRFProtection([]string{"https://app.example"})
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name         string
		method       string
		origin       string
		secFetchSite string
		proof        string
		wantStatus   int
	}{
		{name: "safe cross-origin request", method: http.MethodGet, origin: "https://attacker.example", secFetchSite: "cross-site", wantStatus: http.StatusNoContent},
		{name: "trusted browser request", method: http.MethodPost, origin: "https://app.example", secFetchSite: "cross-site", proof: csrfProofValue, wantStatus: http.StatusNoContent},
		{name: "same-origin request", method: http.MethodPost, origin: "https://api.example", secFetchSite: "same-origin", proof: csrfProofValue, wantStatus: http.StatusNoContent},
		{name: "non-browser client with proof", method: http.MethodPost, proof: csrfProofValue, wantStatus: http.StatusNoContent},
		{name: "missing proof", method: http.MethodPost, origin: "https://app.example", secFetchSite: "same-origin", wantStatus: http.StatusForbidden},
		{name: "cross-origin request", method: http.MethodPost, origin: "https://attacker.example", secFetchSite: "cross-site", proof: csrfProofValue, wantStatus: http.StatusForbidden},
		{name: "null origin", method: http.MethodDelete, origin: "null", proof: csrfProofValue, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, "https://api.example/api/v1/items", nil)
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			if tt.secFetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", tt.secFetchSite)
			}
			if tt.proof != "" {
				request.Header.Set(csrfProofHeader, tt.proof)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}

func TestCSRFProtectionRejectsInvalidTrustedOrigin(t *testing.T) {
	if _, err := NewCSRFProtection([]string{"https://app.example/path"}); err == nil {
		t.Fatal("NewCSRFProtection() error = nil, want an error")
	}
}

func TestRateLimit(t *testing.T) {
	middleware, err := NewRateLimit(2, time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for attempt, wantStatus := range []int{http.StatusNoContent, http.StatusNoContent, http.StatusTooManyRequests} {
		request := httptest.NewRequest(http.MethodGet, "https://api.example/api/v1/events", nil)
		request.RemoteAddr = "198.51.100.10:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != wantStatus {
			t.Fatalf("attempt %d status = %d, want %d", attempt+1, response.Code, wantStatus)
		}
		if wantStatus == http.StatusTooManyRequests && response.Header().Get("Retry-After") == "" {
			t.Fatal("Retry-After header is missing")
		}
	}

	healthRequest := httptest.NewRequest(http.MethodGet, "https://api.example/health", nil)
	healthRequest.RemoteAddr = "198.51.100.10:1234"
	healthResponse := httptest.NewRecorder()
	handler.ServeHTTP(healthResponse, healthRequest)
	if healthResponse.Code != http.StatusNoContent {
		t.Fatalf("health status = %d, want %d", healthResponse.Code, http.StatusNoContent)
	}
}

func TestRateLimitRefillsTokens(t *testing.T) {
	limiter := &ipRateLimiter{
		limit:           1,
		refillPerSecond: 1.0 / 60.0,
		window:          time.Minute,
		buckets:         make(map[string]*clientBucket),
	}
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	if allowed, _ := limiter.allow("client", now); !allowed {
		t.Fatal("first request was rejected")
	}
	if allowed, _ := limiter.allow("client", now); allowed {
		t.Fatal("second immediate request was allowed")
	}
	if allowed, _ := limiter.allow("client", now.Add(time.Minute)); !allowed {
		t.Fatal("request after one window was rejected")
	}
}

func TestRateLimitIsSafeForConcurrentRequests(t *testing.T) {
	const requestLimit = 20
	limiter := &ipRateLimiter{
		limit:           requestLimit,
		refillPerSecond: float64(requestLimit) / time.Hour.Seconds(),
		window:          time.Hour,
		buckets:         make(map[string]*clientBucket),
	}
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	results := make(chan bool, requestLimit*2)
	var requests sync.WaitGroup
	for range requestLimit * 2 {
		requests.Add(1)
		go func() {
			defer requests.Done()
			allowed, _ := limiter.allow("client", now)
			results <- allowed
		}()
	}
	requests.Wait()
	close(results)

	allowedCount := 0
	for allowed := range results {
		if allowed {
			allowedCount++
		}
	}
	if allowedCount != requestLimit {
		t.Fatalf("allowed requests = %d, want %d", allowedCount, requestLimit)
	}
}

func TestClientIPTrustsForwardedHeadersOnlyFromConfiguredProxy(t *testing.T) {
	_, trustedNetwork, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	limiter := &ipRateLimiter{trustedProxies: []*net.IPNet{trustedNetwork}}

	trustedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	trustedRequest.RemoteAddr = "10.0.0.5:1234"
	trustedRequest.Header.Set("X-Forwarded-For", "198.51.100.20, 10.0.0.4")
	if got := limiter.clientIP(trustedRequest); got != "198.51.100.20" {
		t.Fatalf("trusted proxy client IP = %q", got)
	}

	untrustedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	untrustedRequest.RemoteAddr = "203.0.113.7:1234"
	untrustedRequest.Header.Set("X-Forwarded-For", "198.51.100.20")
	if got := limiter.clientIP(untrustedRequest); got != "203.0.113.7" {
		t.Fatalf("untrusted peer client IP = %q", got)
	}
}
