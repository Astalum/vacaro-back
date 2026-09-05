package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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
}

type GoogleConfig struct {
	Enabled             bool
	ClientID            string
	ClientSecret        string
	LoginRedirectURL    string
	CalendarRedirectURL string
	TokenEncryptionKey  string
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

	return Config{
		Port:                port,
		DatabaseURL:         databaseURL,
		SessionKey:          sessionKey,
		SessionCookieSecure: cookieSecure,
		GuestTTL:            guestTTL,
		PublicAppURL:        envOrDefault("PUBLIC_APP_URL", "http://localhost:3000"),
		RegisteredTTL:       registeredTTL,
		Google:              googleConfig,
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
