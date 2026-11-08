package google

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestCodeExchangeSendsBoundPKCEVerifier(t *testing.T) {
	verifier := oauth2.GenerateVerifier()
	client := New(Config{ClientID: "test-client", ClientSecret: "test-secret"})
	authorization, _ := url.Parse(client.CalendarAuthURL("state", verifier))
	challenge := authorization.Query().Get("code_challenge")
	var accepted, rejected int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Error("code exchange must use POST")
		}
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("code") != "one-use-code" {
			t.Error("unexpected token grant")
		}
		received := r.PostForm.Get("code_verifier")
		if received == "" || oauth2.S256ChallengeFromVerifier(received) != challenge {
			rejected++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		accepted++
		_, _ = w.Write([]byte(`{"access_token":"fake-access","refresh_token":"fake-refresh","token_type":"Bearer"}`))
	}))
	defer server.Close()
	client.login.Endpoint.TokenURL = server.URL
	client.calendar.Endpoint.TokenURL = server.URL
	token, _, err := client.ExchangeCalendar(t.Context(), "one-use-code", verifier)
	if err != nil || token != "fake-refresh" {
		t.Fatalf("calendar exchange: %v", err)
	}
	_, _, err = client.ExchangeCalendar(t.Context(), "one-use-code", oauth2.GenerateVerifier())
	if err == nil {
		t.Fatal("substituted verifier must fail")
	}
	// The fixture deliberately omits the ID token; identity validation must still fail after the protected exchange.
	if _, err = client.ExchangeIdentity(t.Context(), "one-use-code", "nonce", verifier); err == nil || !strings.Contains(err.Error(), "ID token") {
		t.Fatalf("login must still require ID token: %v", err)
	}
	if _, err = client.ExchangeIdentity(t.Context(), "one-use-code", "nonce", oauth2.GenerateVerifier()); err == nil {
		t.Fatal("login accepted a substituted verifier")
	}
	if accepted != 2 || rejected < 2 {
		t.Fatalf("exchanges accepted=%d rejected=%d", accepted, rejected)
	}
}

func TestCodeExchangeRejectsMissingOrMalformedVerifierBeforeNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	client := New(Config{ClientID: "test"})
	client.login.Endpoint.TokenURL = server.URL
	client.calendar.Endpoint.TokenURL = server.URL
	for _, verifier := range []string{"", strings.Repeat("a", 42), strings.Repeat("a", 129), strings.Repeat("a", 42) + "!"} {
		if _, _, err := client.ExchangeCalendar(t.Context(), "code", verifier); err == nil {
			t.Fatal("calendar accepted invalid verifier")
		}
		if _, err := client.ExchangeIdentity(t.Context(), "code", "nonce", verifier); err == nil {
			t.Fatal("login accepted invalid verifier")
		}
	}
	if calls != 0 {
		t.Fatal("invalid verifier reached token endpoint")
	}
}
