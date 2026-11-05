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

func TestResponseSessionRoundTrip(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	manager, err := NewManager(key, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	w := httptest.NewRecorder()
	want := Session{UserID: "guest-id", AccountType: "response", EventID: "event-id", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := manager.SetResponse(w, want); err != nil {
		t.Fatalf("SetResponse() error = %v", err)
	}
	cookie := w.Result().Cookies()[0]
	if cookie.Name != ResponseCookieName || cookie.Path != "/api/v1/events/" || !cookie.HttpOnly {
		t.Fatalf("response cookie = %#v", cookie)
	}

	r := httptest.NewRequest("GET", "/api/v1/events/event-id", nil)
	r.AddCookie(cookie)
	got, err := manager.ReadResponse(r)
	if err != nil {
		t.Fatalf("ReadResponse() error = %v", err)
	}
	if got != want {
		t.Errorf("ReadResponse() = %#v, want %#v", got, want)
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

func TestSessionsCannotBeRenamedAcrossScopes(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	manager, err := NewManager(key, false)
	if err != nil {
		t.Fatal(err)
	}
	normal := httptest.NewRecorder()
	if err := manager.Set(normal, Session{UserID: "guest", AccountType: "guest", ExpiresAt: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := manager.SetResponse(response, Session{UserID: "guest", AccountType: "response", EventID: "event", ExpiresAt: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}

	renamedResponse := *response.Result().Cookies()[0]
	renamedResponse.Name = CookieName
	request := httptest.NewRequest("GET", "/api/v1/me", nil)
	request.AddCookie(&renamedResponse)
	if _, err := manager.Read(request); err == nil {
		t.Fatal("response cookie was accepted as a full account session")
	}

	renamedNormal := *normal.Result().Cookies()[0]
	renamedNormal.Name = ResponseCookieName
	request = httptest.NewRequest("GET", "/api/v1/events/event", nil)
	request.AddCookie(&renamedNormal)
	if _, err := manager.ReadResponse(request); err == nil {
		t.Fatal("normal cookie was accepted as a response session")
	}

	if err := manager.Set(httptest.NewRecorder(), Session{UserID: "guest", AccountType: "response", EventID: "event", ExpiresAt: time.Now().Add(time.Hour).Unix()}); err == nil {
		t.Fatal("Set accepted an event-scoped response session")
	}
}
