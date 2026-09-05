package auth

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionRoundTrip(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	manager, err := NewManager(key, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	w := httptest.NewRecorder()
	want := Session{UserID: "user-id", AccountType: "guest", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := manager.Set(w, want); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(w.Result().Cookies()[0])
	got, err := manager.Read(r)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got != want {
		t.Errorf("Read() = %#v, want %#v", got, want)
	}
}

func TestNewManagerRejectsInvalidKeys(t *testing.T) {
	tests := []string{"not-base64", base64.StdEncoding.EncodeToString([]byte("too-short"))}
	for _, key := range tests {
		if _, err := NewManager(key, false); err == nil {
			t.Errorf("NewManager(%q) error = nil, want error", key)
		}
	}
}

func TestTamperedSessionIsRejected(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	manager, _ := NewManager(key, false)
	w := httptest.NewRecorder()
	_ = manager.Set(w, Session{UserID: "user-id", AccountType: "guest", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	cookie := w.Result().Cookies()[0]
	cookie.Value += "tampered"

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	if _, err := manager.Read(r); err == nil {
		t.Fatal("Read() error = nil, want error")
	}
}

func TestClearExpiresCookie(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	manager, _ := NewManager(key, true)
	w := httptest.NewRecorder()
	manager.Clear(w)
	cookie := w.Result().Cookies()[0]
	if cookie.MaxAge != -1 || !cookie.HttpOnly || !cookie.Secure {
		t.Errorf("cleared cookie = %#v", cookie)
	}
}

func TestSessionContext(t *testing.T) {
	want := Session{UserID: "user-id", AccountType: "guest", ExpiresAt: 123}
	ctx := WithSession(context.Background(), want)
	got, ok := FromContext(ctx)
	if !ok || got != want {
		t.Fatalf("FromContext() = %#v, %v; want %#v, true", got, ok, want)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	manager, _ := NewManager(key, false)
	w := httptest.NewRecorder()
	_ = manager.Set(w, Session{UserID: "user-id", AccountType: "guest", ExpiresAt: time.Now().Add(-time.Minute).Unix()})

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(w.Result().Cookies()[0])
	if _, err := manager.Read(r); err == nil {
		t.Fatal("Read() error = nil, want error")
	}
}
