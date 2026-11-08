package google

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFreeBusyUsesAccessibleCalendarScope(t *testing.T) {
	client := New(Config{ClientID: "test"})
	if !strings.Contains(client.CalendarAuthURL("state", strings.Repeat("v", 43)), "calendar.events.freebusy") {
		t.Fatal("missing scope for shared calendars")
	}
}

func TestFreeBusyHandlesProviderFailuresWithoutLosingBusyTime(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantKind string
		wantBusy int
	}{
		{"busy intervals", 200, `{"calendars":{"work":{"busy":[{"start":"2026-11-08T10:00:00+09:00","end":"2026-11-08T11:00:00+09:00"}]},"other":{"busy":[]}}}`, "", 1},
		{"missing calendar", 200, `{"calendars":{"work":{"busy":[]}}}`, "calendar_unavailable", 0},
		{"unavailable subscribed calendar", 200, `{"calendars":{"work":{"busy":[{"start":"2026-11-08T10:00:00Z","end":"2026-11-08T11:00:00Z"}]},"other":{"errors":[{"reason":"notFound"}]}}}`, "calendar_unavailable", 0},
		{"temporary per-calendar failure", 200, `{"calendars":{"work":{"busy":[]},"other":{"errors":[{"reason":"internalError"}]}}}`, "temporarily_unavailable", 0},
		{"missing permission", 403, `{"error":{"errors":[{"reason":"insufficientPermissions"}],"message":"private-calendar@example.com access_token=secret"}}`, "permission_required", 0},
		{"disabled API", 403, `{"error":{"details":[{"reason":"SERVICE_DISABLED"}]}}`, "api_disabled", 0},
		{"rate limited", 403, `{"error":{"errors":[{"reason":"rateLimitExceeded"}]}}`, "temporarily_unavailable", 0},
		{"unauthorized", 401, `{}`, "reauthorization_required", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/token" {
					_, _ = w.Write([]byte(`{"access_token":"test-access","token_type":"Bearer","expires_in":3600}`))
					return
				}
				if r.URL.Path != "/freeBusy" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				var payload struct {
					Items []struct {
						ID string `json:"id"`
					} `json:"items"`
					TimeZone string `json:"timeZone"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if len(payload.Items) != 2 || payload.TimeZone != "Asia/Tokyo" {
					t.Errorf("payload = %#v", payload)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			client := New(Config{ClientID: "test", ClientSecret: "secret"})
			client.calendarAPIBaseURL = server.URL
			client.calendar.Endpoint.TokenURL = server.URL + "/token"
			start := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
			busy, err := client.FreeBusy(t.Context(), "test-refresh", []string{"work", "other"}, start, start.Add(24*time.Hour), "Asia/Tokyo")
			if tt.wantKind == "" {
				if err != nil || len(busy) != tt.wantBusy {
					t.Fatalf("result = %v, %v", busy, err)
				}
				if busy[0].StartAt.Location() != time.UTC || busy[0].StartAt.Hour() != 1 {
					t.Fatalf("UTC normalization = %v", busy[0].StartAt)
				}
			} else {
				var calendarError *CalendarError
				if !errors.As(err, &calendarError) || calendarError.Kind != tt.wantKind {
					t.Fatalf("error = %v, want %s", err, tt.wantKind)
				}
				if busy != nil {
					t.Fatal("must not return partial availability when a selected calendar fails")
				}
				if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "example.com") {
					t.Fatal("provider details leaked")
				}
			}
		})
	}
}

func TestCalendarRefreshTokenRevocationRequiresReconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer server.Close()
	client := New(Config{ClientID: "test"})
	client.calendar.Endpoint.TokenURL = server.URL
	_, err := client.ListCalendars(t.Context(), "revoked-refresh-token")
	var problem *CalendarError
	if !errors.As(err, &problem) || problem.Kind != "reauthorization_required" {
		t.Fatalf("error = %v", err)
	}
}

func TestExchangeCalendarRejectsIncompleteReadPermissions(t *testing.T) {
	for _, scopes := range []string{calendarListScope, calendarFreeBusyScope, calendarListScope + " " + calendarFreeBusyScope, calendarListScope + " https://www.googleapis.com/auth/calendar.freebusy"} {
		t.Run(scopes, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access", "refresh_token": "test-refresh", "token_type": "Bearer", "scope": scopes})
			}))
			defer server.Close()
			client := New(Config{ClientID: "test"})
			client.calendar.Endpoint.TokenURL = server.URL
			token, _, err := client.ExchangeCalendar(t.Context(), "code", strings.Repeat("v", 43))
			if scopes == calendarListScope || scopes == calendarFreeBusyScope {
				var problem *CalendarError
				if !errors.As(err, &problem) || problem.Kind != "permission_required" || token != "" {
					t.Fatalf("partial consent = %q, %v", token, err)
				}
			} else if err != nil || token != "test-refresh" {
				t.Fatalf("read-only consent = %q, %v", token, err)
			}
		})
	}
}

func TestIsHolidayCalendarID(t *testing.T) {
	for _, tt := range []struct {
		id   string
		want bool
	}{
		{"ja.japanese#holiday@group.v.calendar.google.com", true},
		{"ja.japanese.official#holiday@group.v.calendar.google.com", true},
		{"en.usa#holiday@group.v.calendar.google.com", true},
		{" EN.UK#HOLIDAY@GROUP.V.CALENDAR.GOOGLE.COM ", true},
		{"work@group.calendar.google.com", false},
		{"holiday@example.com", false},
		{"ja.japanese#holiday@group.v.calendar.google.com.invalid", false},
		{"#contacts@group.v.calendar.google.com", false},
	} {
		if got := IsHolidayCalendarID(tt.id); got != tt.want {
			t.Errorf("IsHolidayCalendarID(%q) = %v", tt.id, got)
		}
	}
}
