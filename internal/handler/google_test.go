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
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"oauth_failed"`) {
		t.Fatalf("callback response = %d %s", response.Code, response.Body.String())
	}
}
