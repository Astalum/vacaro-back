package auth

import (
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
