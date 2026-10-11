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

const apiRoutesTestKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func newAPIRoutesTestServer(t *testing.T) (*http.ServeMux, *auth.Manager) {
	t.Helper()
	sessions, err := auth.NewManager(apiRoutesTestKey, false)
	if err != nil {
		t.Fatal(err)
	}
	states, err := auth.NewOAuthStateManager(apiRoutesTestKey, false)
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(service.New(nil, time.Hour, "http://localhost:3000"), sessions, states, "http://localhost:3000")
	mux := http.NewServeMux()
	api.Register(mux)
	return mux, sessions
}

func TestAuthenticatedAPIRoutesRequireSession(t *testing.T) {
	mux, _ := newAPIRoutesTestServer(t)
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/me"},
		{http.MethodPatch, "/api/v1/me"},
		{http.MethodDelete, "/api/v1/session"},
		{http.MethodPost, "/api/v1/events"},
		{http.MethodGet, "/api/v1/events"},
		{http.MethodGet, "/api/v1/events/event-1"},
		{http.MethodPatch, "/api/v1/events/event-1"},
		{http.MethodPatch, "/api/v1/events/event-1/results-visibility"},
		{http.MethodDelete, "/api/v1/events/event-1"},
		{http.MethodPost, "/api/v1/events/event-1/invite-token"},
		{http.MethodPut, "/api/v1/events/event-1/confirmation"},
		{http.MethodDelete, "/api/v1/events/event-1/confirmation"},
		{http.MethodGet, "/api/v1/events/event-1/confirmation-calendar"},
		{http.MethodPost, "/api/v1/events/event-1/confirmation-calendar"},
		{http.MethodGet, "/api/v1/events/event-1/groups"},
		{http.MethodPost, "/api/v1/events/event-1/groups"},
		{http.MethodGet, "/api/v1/events/event-1/groups/group-1"},
		{http.MethodPatch, "/api/v1/events/event-1/groups/group-1"},
		{http.MethodDelete, "/api/v1/events/event-1/groups/group-1"},
		{http.MethodPut, "/api/v1/events/event-1/groups/group-1/members"},
		{http.MethodGet, "/api/v1/events/event-1/groups/group-1/availability"},
		{http.MethodGet, "/api/v1/events/event-1/groups/group-1/candidates"},
		{http.MethodPut, "/api/v1/events/event-1/groups/group-1/confirmation"},
		{http.MethodDelete, "/api/v1/events/event-1/groups/group-1/confirmation"},
		{http.MethodPost, "/api/v1/invitations/token-1/participants"},
		{http.MethodGet, "/api/v1/events/event-1/participants"},
		{http.MethodDelete, "/api/v1/events/event-1/participants/me"},
		{http.MethodGet, "/api/v1/events/event-1/my-availability"},
		{http.MethodPut, "/api/v1/events/event-1/my-availability"},
		{http.MethodPost, "/api/v1/events/event-1/my-availability/copy"},
		{http.MethodGet, "/api/v1/events/event-1/availability"},
		{http.MethodGet, "/api/v1/events/event-1/candidates"},
		{http.MethodPost, "/api/v1/integrations/google-calendar"},
		{http.MethodGet, "/api/v1/integrations/google-calendar/calendars"},
		{http.MethodDelete, "/api/v1/integrations/google-calendar"},
		{http.MethodPost, "/api/v1/events/event-1/google-calendar-availability/preview"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"code":"authentication_required"`) {
				t.Errorf("body = %s, want authentication_required", response.Body.String())
			}
		})
	}
}

func TestJSONAPIRoutesRejectMalformedBodies(t *testing.T) {
	mux, sessions := newAPIRoutesTestServer(t)
	tests := []struct {
		method        string
		path          string
		authenticated bool
	}{
		{http.MethodPost, "/api/v1/guest-sessions", false},
		{http.MethodPatch, "/api/v1/me", true},
		{http.MethodPost, "/api/v1/events", true},
		{http.MethodPatch, "/api/v1/events/event-1", true},
		{http.MethodPatch, "/api/v1/events/event-1/results-visibility", true},
		{http.MethodPut, "/api/v1/events/event-1/confirmation", true},
		{http.MethodPost, "/api/v1/events/event-1/confirmation-calendar", true},
		{http.MethodPost, "/api/v1/invitations/token-1/participants", true},
		{http.MethodPut, "/api/v1/events/event-1/my-availability", true},
		{http.MethodPost, "/api/v1/events/event-1/my-availability/copy", true},
		{http.MethodPost, "/api/v1/events/event-1/groups", true},
		{http.MethodPatch, "/api/v1/events/event-1/groups/group-1", true},
		{http.MethodPut, "/api/v1/events/event-1/groups/group-1/members", true},
		{http.MethodPut, "/api/v1/events/event-1/groups/group-1/confirmation", true},
		{http.MethodPost, "/api/v1/events/event-1/google-calendar-availability/preview", true},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"broken":`))
			request.Header.Set("Content-Type", "application/json")
			if tt.authenticated {
				cookieResponse := httptest.NewRecorder()
				err := sessions.Set(cookieResponse, auth.Session{
					UserID: "user-1", AccountType: "guest", ExpiresAt: time.Now().Add(time.Hour).Unix(),
				})
				if err != nil {
					t.Fatal(err)
				}
				request.AddCookie(cookieResponse.Result().Cookies()[0])
			}

			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"code":"invalid_json"`) {
				t.Errorf("body = %s, want invalid_json", response.Body.String())
			}
		})
	}
}
