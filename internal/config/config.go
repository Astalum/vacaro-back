package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                string
	DatabaseURL         string
	SessionKey          string
	SessionCookieSecure bool
	GuestTTL            time.Duration
	PublicAppURL        string
	RegisteredTTL       time.Duration
	Google              GoogleConfig
	Security            SecurityConfig
}

type GoogleConfig struct {
	Enabled             bool
	ClientID            string
	ClientSecret        string
	LoginRedirectURL    string
	CalendarRedirectURL string
	TokenEncryptionKey  string
}

type SecurityConfig struct {
	TrustedOrigins    []string
	RateLimitRequests int
	RateLimitWindow   time.Duration
	TrustedProxyCIDRs []string
}

func Load() (Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		return Config{}, errors.New("SESSION_KEY is required")
	}

	cookieSecure, err := strconv.ParseBool(envOrDefault("SESSION_COOKIE_SECURE", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("parse SESSION_COOKIE_SECURE: %w", err)
	}
	guestTTL, err := time.ParseDuration(envOrDefault("GUEST_TTL", "168h"))
	if err != nil || guestTTL <= 0 {
		return Config{}, errors.New("GUEST_TTL must be a positive duration")
	}
	registeredTTL, err := time.ParseDuration(envOrDefault("REGISTERED_SESSION_TTL", "720h"))
	if err != nil || registeredTTL <= 0 {
		return Config{}, errors.New("REGISTERED_SESSION_TTL must be a positive duration")
	}
	googleConfig, err := loadGoogleConfig()
	if err != nil {
		return Config{}, err
	}
	publicAppURL := envOrDefault("PUBLIC_APP_URL", "http://localhost:3000")
	securityConfig, err := loadSecurityConfig(publicAppURL)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:                port,
		DatabaseURL:         databaseURL,
		SessionKey:          sessionKey,
		SessionCookieSecure: cookieSecure,
		GuestTTL:            guestTTL,
		PublicAppURL:        publicAppURL,
		RegisteredTTL:       registeredTTL,
		Google:              googleConfig,
		Security:            securityConfig,
	}, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func loadGoogleConfig() (GoogleConfig, error) {
	config := GoogleConfig{
		ClientID:            os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret:        os.Getenv("GOOGLE_CLIENT_SECRET"),
		LoginRedirectURL:    os.Getenv("GOOGLE_LOGIN_REDIRECT_URL"),
		CalendarRedirectURL: os.Getenv("GOOGLE_CALENDAR_REDIRECT_URL"),
		TokenEncryptionKey:  os.Getenv("TOKEN_ENCRYPTION_KEY"),
	}
	values := []string{config.ClientID, config.ClientSecret, config.LoginRedirectURL, config.CalendarRedirectURL, config.TokenEncryptionKey}
	configured := 0
	for _, value := range values {
		if value != "" {
			configured++
		}
	}
	if configured == 0 {
		return config, nil
	}
	if configured != len(values) {
		return GoogleConfig{}, errors.New("all GOOGLE_* and TOKEN_ENCRYPTION_KEY settings are required when Google integration is enabled")
	}
	config.Enabled = true
	return config, nil
}

func loadSecurityConfig(publicAppURL string) (SecurityConfig, error) {
	origins := splitCSV(os.Getenv("ALLOWED_ORIGINS"))
	if len(origins) == 0 {
		origins = []string{publicAppURL}
	}
	trustedOrigins := make([]string, 0, len(origins))
	seenOrigins := make(map[string]struct{}, len(origins))
	for _, value := range origins {
		origin, err := normalizeOrigin(value)
		if err != nil {
			return SecurityConfig{}, fmt.Errorf("parse ALLOWED_ORIGINS: %w", err)
		}
		if _, exists := seenOrigins[origin]; exists {
			continue
		}
		seenOrigins[origin] = struct{}{}
		trustedOrigins = append(trustedOrigins, origin)
	}

	rateLimitRequests, err := strconv.Atoi(envOrDefault("API_RATE_LIMIT_REQUESTS", "120"))
	if err != nil || rateLimitRequests <= 0 {
		return SecurityConfig{}, errors.New("API_RATE_LIMIT_REQUESTS must be a positive integer")
	}
	rateLimitWindow, err := time.ParseDuration(envOrDefault("API_RATE_LIMIT_WINDOW", "1m"))
	if err != nil || rateLimitWindow <= 0 {
		return SecurityConfig{}, errors.New("API_RATE_LIMIT_WINDOW must be a positive duration")
	}

	trustedProxyCIDRs := splitCSV(os.Getenv("TRUSTED_PROXY_CIDRS"))
	for _, value := range trustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(value); err != nil {
			return SecurityConfig{}, fmt.Errorf("parse TRUSTED_PROXY_CIDRS %q: %w", value, err)
		}
	}

	return SecurityConfig{
		TrustedOrigins:    trustedOrigins,
		RateLimitRequests: rateLimitRequests,
		RateLimitWindow:   rateLimitWindow,
		TrustedProxyCIDRs: trustedProxyCIDRs,
	}, nil
}

func normalizeOrigin(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q must be an HTTP(S) origin without a path, query, or fragment", value)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
