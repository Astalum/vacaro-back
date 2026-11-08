package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testEncryptionKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestOAuthStateRoundTrip(t *testing.T) {
	manager, err := NewOAuthStateManager(testEncryptionKey, false)
	if err != nil {
		t.Fatal(err)
	}
	beginResponse := httptest.NewRecorder()
	want, err := manager.Begin(beginResponse, "google_login", "guest-id")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/auth/google/callback?state="+want.State, nil)
	request.AddCookie(beginResponse.Result().Cookies()[0])
	consumeResponse := httptest.NewRecorder()
	got, err := manager.Consume(consumeResponse, request, want.State, "google_login")
	if err != nil {
		t.Fatal(err)
	}
	if got.Nonce == "" || got.UserID != "guest-id" || got.State != want.State {
		t.Fatalf("Consume() = %#v", got)
	}
	if cookies := consumeResponse.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("state cookie was not cleared: %#v", cookies)
	}
}

func TestOAuthStateRejectsMismatch(t *testing.T) {
	manager, err := NewOAuthStateManager(testEncryptionKey, false)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	state, err := manager.Begin(response, "google_login", "")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/auth/google/callback", nil)
	request.AddCookie(response.Result().Cookies()[0])
	if _, err := manager.Consume(httptest.NewRecorder(), request, state.State+"tampered", "google_login"); err == nil {
		t.Fatal("Consume() error = nil, want state mismatch error")
	}
}

func TestBoxRejectsTamperedCiphertext(t *testing.T) {
	box, err := NewBox(testEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte("refresh-token"))
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := box.Open(sealed); err == nil {
		t.Fatal("Open() error = nil, want authentication error")
	}
}

func TestOAuthStateUsesIndependentPKCEVerifier(t *testing.T) {
	manager, err := NewOAuthStateManager(testEncryptionKey, true)
	if err != nil {
		t.Fatal(err)
	}
	firstResponse := httptest.NewRecorder()
	first, err := manager.Begin(firstResponse, "google_login", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Begin(httptest.NewRecorder(), "google_calendar", "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.CodeVerifier) != 43 || first.CodeVerifier == first.State || first.CodeVerifier == first.Nonce || first.CodeVerifier == second.CodeVerifier {
		t.Fatal("PKCE verifier must be independent and fresh")
	}
	cookie := firstResponse.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("OAuth cookie is missing browser protections")
	}
	request := httptest.NewRequest("GET", "/callback", nil)
	request.AddCookie(cookie)
	got, err := manager.Consume(httptest.NewRecorder(), request, first.State, "google_login")
	if err != nil || got.CodeVerifier != first.CodeVerifier {
		t.Fatalf("verifier was not preserved: %v", err)
	}
}

func TestOAuthStateRejectsLegacyStateWithoutPKCE(t *testing.T) {
	manager, _ := NewOAuthStateManager(testEncryptionKey, false)
	response := httptest.NewRecorder()
	state, _ := manager.Begin(response, "google_login", "")
	state.CodeVerifier = ""
	plain, _ := json.Marshal(state)
	box, _ := NewBox(testEncryptionKey)
	sealed, _ := box.Seal(plain)
	request := httptest.NewRequest("GET", "/callback", nil)
	request.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: base64.RawURLEncoding.EncodeToString(sealed)})
	if _, err := manager.Consume(httptest.NewRecorder(), request, state.State, "google_login"); err == nil {
		t.Fatal("must not downgrade to a flow without PKCE")
	}
}
