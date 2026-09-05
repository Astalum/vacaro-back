package config

import (
	"strings"
	"testing"
)

func clearGoogleConfig(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_LOGIN_REDIRECT_URL", "GOOGLE_CALENDAR_REDIRECT_URL", "TOKEN_ENCRYPTION_KEY"} {
		t.Setenv(key, "")
	}
}

func TestLoad(t *testing.T) {
	clearGoogleConfig(t)
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://user:password@db:5432/app")
	t.Setenv("SESSION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	t.Setenv("GUEST_TTL", "24h")
	t.Setenv("PUBLIC_APP_URL", "http://localhost:3000")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.Port != "9090" {
		t.Errorf("Port = %q, want %q", got.Port, "9090")
	}
	if got.DatabaseURL != "postgres://user:password@db:5432/app" {
		t.Errorf("DatabaseURL = %q", got.DatabaseURL)
	}
	if got.GuestTTL.String() != "24h0m0s" {
		t.Errorf("GuestTTL = %s", got.GuestTTL)
	}
}

func TestLoadUsesDefaultPort(t *testing.T) {
	clearGoogleConfig(t)
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "postgres://user:password@db:5432/app")
	t.Setenv("SESSION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.Port != "8080" {
		t.Errorf("Port = %q, want %q", got.Port, "8080")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	clearGoogleConfig(t)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SESSION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
}

func TestLoadRequiresSessionKey(t *testing.T) {
	clearGoogleConfig(t)
	t.Setenv("DATABASE_URL", "postgres://user:password@db:5432/app")
	t.Setenv("SESSION_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
}

func TestLoadRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name     string
		secure   string
		guestTTL string
	}{
		{name: "invalid secure flag", secure: "not-bool", guestTTL: "24h"},
		{name: "invalid duration", secure: "false", guestTTL: "forever"},
		{name: "non-positive duration", secure: "false", guestTTL: "0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearGoogleConfig(t)
			t.Setenv("DATABASE_URL", "postgres://user:password@db:5432/app")
			t.Setenv("SESSION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
			t.Setenv("SESSION_COOKIE_SECURE", tt.secure)
			t.Setenv("GUEST_TTL", tt.guestTTL)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}
}

func TestLoadGoogleConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:password@db:5432/app")
	t.Setenv("SESSION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "client-secret")
	t.Setenv("GOOGLE_LOGIN_REDIRECT_URL", "http://localhost/api/v1/auth/google/callback")
	t.Setenv("GOOGLE_CALENDAR_REDIRECT_URL", "http://localhost/api/v1/integrations/google-calendar/callback")
	t.Setenv("TOKEN_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Google.Enabled || got.RegisteredTTL.String() != "720h0m0s" {
		t.Fatalf("Google = %#v, RegisteredTTL = %s", got.Google, got.RegisteredTTL)
	}
}

func TestLoadRejectsPartialGoogleConfig(t *testing.T) {
	clearGoogleConfig(t)
	t.Setenv("DATABASE_URL", "postgres://user:password@db:5432/app")
	t.Setenv("SESSION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "all GOOGLE_*") {
		t.Fatalf("Load() error = %v", err)
	}
}
