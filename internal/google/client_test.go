package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationURLs(t *testing.T) {
	client := New(Config{
		ClientID: "client-id", ClientSecret: "client-secret",
		LoginRedirectURL: "http://localhost/login/callback", CalendarRedirectURL: "http://localhost/calendar/callback",
	})

	loginURL, err := url.Parse(client.LoginAuthURL("login-state", "login-nonce"))
	if err != nil {
		t.Fatal(err)
	}
	loginQuery := loginURL.Query()
	if loginQuery.Get("state") != "login-state" || loginQuery.Get("nonce") != "login-nonce" || loginQuery.Get("redirect_uri") != "http://localhost/login/callback" {
		t.Fatalf("login query = %v", loginQuery)
	}
	loginScopes := strings.Fields(loginQuery.Get("scope"))
	for _, want := range []string{"openid", "email", "profile"} {
		if !contains(loginScopes, want) {
			t.Errorf("login scopes = %#v, missing %q", loginScopes, want)
		}
	}

	calendarURL, err := url.Parse(client.CalendarAuthURL("calendar-state"))
	if err != nil {
		t.Fatal(err)
	}
	calendarQuery := calendarURL.Query()
	calendarScopes := strings.Fields(calendarQuery.Get("scope"))
	if calendarQuery.Get("state") != "calendar-state" || calendarQuery.Get("access_type") != "offline" || calendarQuery.Get("prompt") != "consent" || !contains(calendarScopes, calendarFreeBusyScope) || !contains(calendarScopes, calendarListScope) || !contains(calendarScopes, CalendarEventsScope) {
		t.Fatalf("calendar query = %v", calendarQuery)
	}
}

func TestUpsertCalendarEventCreatesThenUpdatesExistingEvent(t *testing.T) {
	methods := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token","token_type":"Bearer"}`))
		case "/calendars/primary/events":
			methods = append(methods, r.Method+" "+r.URL.RequestURI())
			w.WriteHeader(http.StatusConflict)
		case "/calendars/primary/events/vacaro123":
			methods = append(methods, r.Method+" "+r.URL.RequestURI())
			var payload struct {
				ID      string `json:"id"`
				Summary string `json:"summary"`
				Start   struct {
					DateTime string `json:"dateTime"`
					TimeZone string `json:"timeZone"`
				} `json:"start"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ID != "vacaro123" || payload.Summary != "定例" || payload.Start.DateTime != "2026-10-10T10:15:00+09:00" || payload.Start.TimeZone != "Asia/Tokyo" {
				t.Fatalf("payload = %#v", payload)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(Config{ClientID: "client-id", ClientSecret: "client-secret"})
	client.calendar.Endpoint.TokenURL = server.URL + "/token"
	client.calendarAPIBaseURL = server.URL
	startAt := time.Date(2026, 10, 10, 10, 15, 0, 0, time.FixedZone("JST", 9*60*60))
	if err := client.UpsertCalendarEvent(t.Context(), "refresh-token", CalendarEvent{
		ID: "vacaro123", Summary: "定例", Description: "vacaroで確定", StartAt: startAt,
		EndAt: startAt.Add(time.Hour), TimeZone: "Asia/Tokyo",
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /calendars/primary/events?sendUpdates=none",
		"PUT /calendars/primary/events/vacaro123?sendUpdates=none",
	}
	if len(methods) != len(want) || methods[0] != want[0] || methods[1] != want[1] {
		t.Fatalf("requests = %#v, want %#v", methods, want)
	}
}

func TestDeleteCalendarEventAllowsMissingEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token","token_type":"Bearer"}`))
			return
		}
		if r.Method != http.MethodDelete || r.URL.RequestURI() != "/calendars/primary/events/vacaro123?sendUpdates=none" {
			t.Fatalf("request = %s %s", r.Method, r.URL.RequestURI())
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(Config{ClientID: "client-id", ClientSecret: "client-secret"})
	client.calendar.Endpoint.TokenURL = server.URL + "/token"
	client.calendarAPIBaseURL = server.URL
	if err := client.DeleteCalendarEvent(t.Context(), "refresh-token", "vacaro123"); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
