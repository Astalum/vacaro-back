package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/auth"
	"github.com/astalum/vacaro-back/internal/service"
)

const googleHandlerTestKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestGoogleRoutesWhenIntegrationDisabled(t *testing.T) {
	sessions, err := auth.NewManager(googleHandlerTestKey, false)
	if err != nil {
		t.Fatal(err)
	}
	states, err := auth.NewOAuthStateManager(googleHandlerTestKey, false)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(service.New(nil, time.Hour, "http://localhost:3000"), sessions, states, "http://localhost:3000")
	mux := http.NewServeMux()
	api.Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/start", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"google_not_configured"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "http://localhost:3000?google_login=error" {
		t.Fatalf("callback response = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestGoogleLoginCallbackReplayRedirectsAuthenticatedUser(t *testing.T) {
	sessions, err := auth.NewManager(googleHandlerTestKey, false)
	if err != nil {
		t.Fatal(err)
	}
	states, err := auth.NewOAuthStateManager(googleHandlerTestKey, false)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(service.New(nil, time.Hour, "http://localhost:3000"), sessions, states, "http://localhost:3000")
	mux := http.NewServeMux()
	api.Register(mux)

	cookieResponse := httptest.NewRecorder()
	if err := sessions.Set(cookieResponse, auth.Session{
		UserID: "registered-user", AccountType: "registered", ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?state=already-consumed&code=already-used", nil)
	request.AddCookie(cookieResponse.Result().Cookies()[0])
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "http://localhost:3000?google_login=success" {
		t.Fatalf("Location = %q", location)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestGoogleLoginProviderErrorIsNotTreatedAsReplay(t *testing.T) {
	sessions, err := auth.NewManager(googleHandlerTestKey, false)
	if err != nil {
		t.Fatal(err)
	}
	states, err := auth.NewOAuthStateManager(googleHandlerTestKey, false)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(service.New(nil, time.Hour, "http://localhost:3000"), sessions, states, "http://localhost:3000")
	mux := http.NewServeMux()
	api.Register(mux)

	cookieResponse := httptest.NewRecorder()
	if err := sessions.Set(cookieResponse, auth.Session{
		UserID: "registered-user", AccountType: "registered", ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?state=state&error=access_denied", nil)
	request.AddCookie(cookieResponse.Result().Cookies()[0])
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "http://localhost:3000?google_login=error" {
		t.Fatalf("Location = %q", location)
	}
}
