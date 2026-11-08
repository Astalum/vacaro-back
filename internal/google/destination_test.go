package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestEnsureCalendarLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, current   string
		status, creates int
		want            string
		fail            bool
	}{
		{"first connection", "", 0, 1, "dedicated@group.calendar.google.com", false},
		{"reconnect reuses saved ID", "saved@group.calendar.google.com", 200, 0, "saved@group.calendar.google.com", false},
		{"deleted calendar is replaced", "deleted@group.calendar.google.com", 404, 1, "dedicated@group.calendar.google.com", false},
		{"permission error does not duplicate", "saved@group.calendar.google.com", 403, 0, "", true},
		{"temporary error does not duplicate", "saved@group.calendar.google.com", 503, 0, "", true},
		{"primary is never adopted", "primary", 0, 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/token":
					_, _ = w.Write([]byte(`{"access_token":"test","token_type":"Bearer"}`))
				case r.Method == http.MethodGet && r.URL.Path == "/calendars/"+tc.current:
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"id":"saved","summary":"Renamed by user"}`))
				case r.Method == http.MethodPost && r.URL.Path == "/calendars":
					creates++
					var payload map[string]string
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					if payload["summary"] != "Vacaro" || payload["timeZone"] != "Asia/Tokyo" {
						t.Errorf("creation payload = %v", payload)
					}
					_, _ = w.Write([]byte(`{"id":"dedicated@group.calendar.google.com"}`))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := New(Config{ClientID: "test", ClientSecret: "secret"})
			client.calendar.Endpoint.TokenURL = server.URL + "/token"
			client.calendarAPIBaseURL = server.URL
			got, err := client.EnsureCalendar(t.Context(), "refresh", tc.current, "Asia/Tokyo")
			if (err != nil) != tc.fail || got != tc.want || creates != tc.creates {
				t.Fatalf("result = %q, %v; creates = %d", got, err, creates)
			}
		})
	}
}

func TestCalendarWritesRejectPrimaryAndMissingDestination(t *testing.T) {
	client := New(Config{})
	for _, id := range []string{"", "primary", " PRIMARY "} {
		if err := client.UpsertCalendarEvent(t.Context(), "refresh", CalendarEvent{CalendarID: id}); err == nil {
			t.Errorf("upsert accepted destination %q", id)
		}
		if err := client.DeleteCalendarEvent(t.Context(), "refresh", id, "event"); err == nil {
			t.Errorf("delete accepted destination %q", id)
		}
	}
}

func TestCalendarCreationErrorDoesNotExposeProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"test","token_type":"Bearer"}`))
			return
		}
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":{"message":"private account and credentials"}}`))
	}))
	defer server.Close()
	client := New(Config{})
	client.calendar.Endpoint.TokenURL = server.URL + "/token"
	client.calendar.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	client.calendarAPIBaseURL = server.URL
	_, err := client.EnsureCalendar(t.Context(), "refresh", "", "Asia/Tokyo")
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("error = %v", err)
	}
}
