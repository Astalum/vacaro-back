package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type envelope[T any] struct {
	Data T `json:"data"`
}

type user struct {
	ID string `json:"id"`
}

type event struct {
	ID               string     `json:"id"`
	InviteToken      string     `json:"invite_token"`
	Status           string     `json:"status"`
	SchedulingMode   string     `json:"scheduling_mode"`
	ConfirmedStartAt *time.Time `json:"confirmed_start_at"`
}

type participant struct {
	ID string `json:"id"`
}

type schedulingGroup struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	DurationMinutes  int           `json:"duration_minutes"`
	Members          []participant `json:"members"`
	ConfirmedStartAt *time.Time    `json:"confirmed_start_at"`
}

func TestMVPFlow(t *testing.T) {
	baseURL := os.Getenv("E2E_BASE_URL")
	databaseURL := os.Getenv("E2E_DATABASE_URL")
	if baseURL == "" || databaseURL == "" {
		t.Skip("E2E_BASE_URL and E2E_DATABASE_URL are required")
	}

	organizer := newClient(t)
	member := newClient(t)
	organizerUser := createGuest(t, organizer, baseURL, "E2E Organizer")
	memberUser := createGuest(t, member, baseURL, "E2E Member")

	var eventID string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			t.Logf("cleanup connection failed: %v", err)
			return
		}
		defer pool.Close()
		if eventID != "" {
			_, _ = pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
		}
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1 OR id = $2", organizerUser.ID, memberUser.ID)
	})

	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Minute)
	end := start.Add(4 * time.Hour)
	request := map[string]any{
		"title": "E2E Event", "description": "temporary test event", "time_zone": "Asia/Tokyo",
		"window_start_at": start, "window_end_at": end, "duration_minutes": 60,
		"response_deadline_at": start.Add(-time.Hour),
	}
	created := doJSON[event](t, organizer, http.MethodPost, baseURL+"/api/v1/events", request, http.StatusCreated)
	eventID = created.ID
	if created.InviteToken == "" {
		t.Fatal("invite token is empty")
	}

	doJSON[event](t, http.DefaultClient, http.MethodGet, baseURL+"/api/v1/invitations/"+created.InviteToken, nil, http.StatusOK)
	doJSON[map[string]any](t, member, http.MethodPost, baseURL+"/api/v1/invitations/"+created.InviteToken+"/participants", map[string]string{"display_name": "E2E Member"}, http.StatusCreated)

	availability := map[string]any{"intervals": []map[string]time.Time{{"start_at": start, "end_at": start.Add(2 * time.Hour)}}}
	doJSON[map[string]any](t, organizer, http.MethodPut, baseURL+"/api/v1/events/"+eventID+"/my-availability", availability, http.StatusOK)
	doJSON[map[string]any](t, member, http.MethodPut, baseURL+"/api/v1/events/"+eventID+"/my-availability", availability, http.StatusOK)

	candidates := doJSON[[]struct {
		AvailableCount int `json:"available_participant_count"`
	}](t, organizer, http.MethodGet, baseURL+"/api/v1/events/"+eventID+"/candidates", nil, http.StatusOK)
	if len(candidates) == 0 || candidates[0].AvailableCount != 2 {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}

	confirmed := doJSON[event](t, organizer, http.MethodPut, baseURL+"/api/v1/events/"+eventID+"/confirmation", map[string]time.Time{"start_at": start, "end_at": start.Add(time.Hour)}, http.StatusOK)
	if confirmed.Status != "confirmed" {
		t.Fatalf("status = %q, want confirmed", confirmed.Status)
	}
	doJSON[event](t, organizer, http.MethodDelete, baseURL+"/api/v1/events/"+eventID+"/confirmation", nil, http.StatusOK)
	doNoContent(t, organizer, http.MethodDelete, baseURL+"/api/v1/events/"+eventID)
}

func TestCopyAvailabilityFlow(t *testing.T) {
	baseURL := os.Getenv("E2E_BASE_URL")
	databaseURL := os.Getenv("E2E_DATABASE_URL")
	sessionKey := os.Getenv("SESSION_KEY")
	if baseURL == "" || databaseURL == "" || sessionKey == "" {
		t.Skip("E2E_BASE_URL, E2E_DATABASE_URL and SESSION_KEY are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (account_type, display_name, email)
		VALUES ('registered', 'E2E Google User', 'copy-e2e@example.com')
		RETURNING id::text
	`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO auth_accounts (user_id, provider, provider_subject, email)
		VALUES ($1, 'google', $2, 'copy-e2e@example.com')
	`, userID, "e2e-copy-"+userID); err != nil {
		t.Fatal(err)
	}
	eventIDs := make([]string, 0, 2)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		for _, eventID := range eventIDs {
			_, _ = pool.Exec(cleanupCtx, "DELETE FROM events WHERE id = $1", eventID)
		}
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userID)
	})

	client := newClient(t)
	manager, err := auth.NewManager(sessionKey, false)
	if err != nil {
		t.Fatal(err)
	}
	cookieResponse := httptest.NewRecorder()
	if err := manager.Set(cookieResponse, auth.Session{UserID: userID, AccountType: "registered", ExpiresAt: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar.SetCookies(parsedBaseURL, cookieResponse.Result().Cookies())

	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Minute)
	source := createE2EEvent(t, client, baseURL, "Copy Source", start, start.Add(4*time.Hour))
	target := createE2EEvent(t, client, baseURL, "Copy Target", start.Add(2*time.Hour), start.Add(6*time.Hour))
	eventIDs = append(eventIDs, source.ID, target.ID)
	doJSON[map[string]any](t, client, http.MethodPut, baseURL+"/api/v1/events/"+source.ID+"/my-availability", map[string]any{
		"intervals": []map[string]time.Time{{"start_at": start.Add(time.Hour), "end_at": start.Add(3 * time.Hour)}},
	}, http.StatusOK)
	doJSON[map[string]any](t, client, http.MethodPut, baseURL+"/api/v1/events/"+target.ID+"/my-availability", map[string]any{
		"intervals": []map[string]time.Time{{"start_at": start.Add(4 * time.Hour), "end_at": start.Add(5 * time.Hour)}},
	}, http.StatusOK)

	result := doJSON[struct {
		ResponseStatus string `json:"response_status"`
		Intervals      []struct {
			StartAt time.Time `json:"start_at"`
			EndAt   time.Time `json:"end_at"`
		} `json:"intervals"`
	}](t, client, http.MethodPost, baseURL+"/api/v1/events/"+target.ID+"/my-availability/copy", map[string]string{"source_event_id": source.ID}, http.StatusOK)
	if result.ResponseStatus != "submitted" || len(result.Intervals) != 2 {
		t.Fatalf("copy result = %#v", result)
	}
	if !result.Intervals[0].StartAt.Equal(start.Add(2*time.Hour)) || !result.Intervals[0].EndAt.Equal(start.Add(3*time.Hour)) {
		t.Fatalf("clipped interval = %#v", result.Intervals[0])
	}
}

func TestSchedulingGroupFlow(t *testing.T) {
	baseURL := os.Getenv("E2E_BASE_URL")
	databaseURL := os.Getenv("E2E_DATABASE_URL")
	if baseURL == "" || databaseURL == "" {
		t.Skip("E2E_BASE_URL and E2E_DATABASE_URL are required")
	}

	organizer := newClient(t)
	organizerUser := createGuest(t, organizer, baseURL, "Group E2E Organizer")
	var eventID string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			t.Logf("cleanup connection failed: %v", err)
			return
		}
		defer pool.Close()
		if eventID != "" {
			_, _ = pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
		}
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", organizerUser.ID)
	})

	start := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Minute)
	created := doJSON[event](t, organizer, http.MethodPost, baseURL+"/api/v1/events", map[string]any{
		"title": "Group E2E Event", "time_zone": "Asia/Tokyo",
		"window_start_at": start, "window_end_at": start.Add(4 * time.Hour),
		"duration_minutes": 60, "scheduling_mode": "groups",
	}, http.StatusCreated)
	eventID = created.ID
	if created.SchedulingMode != "groups" {
		t.Fatalf("scheduling_mode = %q, want groups", created.SchedulingMode)
	}

	participants := doJSON[[]participant](t, organizer, http.MethodGet, baseURL+"/api/v1/events/"+eventID+"/participants", nil, http.StatusOK)
	if len(participants) != 1 {
		t.Fatalf("participants = %#v", participants)
	}
	duration := 30
	group := doJSON[schedulingGroup](t, organizer, http.MethodPost, baseURL+"/api/v1/events/"+eventID+"/groups", map[string]any{
		"name": " Design ", "duration_minutes": duration,
		"participant_ids": []string{participants[0].ID},
	}, http.StatusCreated)
	if group.Name != "Design" || group.DurationMinutes != duration || len(group.Members) != 1 {
		t.Fatalf("created group = %#v", group)
	}

	doJSON[map[string]any](t, organizer, http.MethodPut, baseURL+"/api/v1/events/"+eventID+"/my-availability", map[string]any{
		"intervals": []map[string]time.Time{{"start_at": start, "end_at": start.Add(2 * time.Hour)}},
	}, http.StatusOK)
	candidates := doJSON[[]struct {
		AvailableCount int `json:"available_participant_count"`
	}](t, organizer, http.MethodGet, baseURL+"/api/v1/events/"+eventID+"/groups/"+group.ID+"/candidates", nil, http.StatusOK)
	if len(candidates) == 0 || candidates[0].AvailableCount != 1 {
		t.Fatalf("group candidates = %#v", candidates)
	}

	confirmed := doJSON[schedulingGroup](t, organizer, http.MethodPut, baseURL+"/api/v1/events/"+eventID+"/groups/"+group.ID+"/confirmation", map[string]time.Time{
		"start_at": start, "end_at": start.Add(30 * time.Minute),
	}, http.StatusOK)
	if confirmed.ConfirmedStartAt == nil {
		t.Fatal("group was not confirmed")
	}
	confirmedEvent := doJSON[event](t, organizer, http.MethodGet, baseURL+"/api/v1/events/"+eventID, nil, http.StatusOK)
	if confirmedEvent.Status != "confirmed" {
		t.Fatalf("event status = %q, want confirmed", confirmedEvent.Status)
	}

	doJSON[schedulingGroup](t, organizer, http.MethodDelete, baseURL+"/api/v1/events/"+eventID+"/groups/"+group.ID+"/confirmation", nil, http.StatusOK)
	reopenedEvent := doJSON[event](t, organizer, http.MethodGet, baseURL+"/api/v1/events/"+eventID, nil, http.StatusOK)
	if reopenedEvent.Status != "open" {
		t.Fatalf("event status after reopen = %q, want open", reopenedEvent.Status)
	}

	confirmedWhole := doJSON[event](t, organizer, http.MethodPut, baseURL+"/api/v1/events/"+eventID+"/confirmation", map[string]time.Time{
		"start_at": start, "end_at": start.Add(time.Hour),
	}, http.StatusOK)
	if confirmedWhole.ConfirmedStartAt == nil || confirmedWhole.Status != "confirmed" {
		t.Fatalf("whole group-mode event was not confirmed: %#v", confirmedWhole)
	}
	doJSON[schedulingGroup](t, organizer, http.MethodPut, baseURL+"/api/v1/events/"+eventID+"/groups/"+group.ID+"/confirmation", map[string]time.Time{
		"start_at": start, "end_at": start.Add(30 * time.Minute),
	}, http.StatusOK)
	reopenedWhole := doJSON[event](t, organizer, http.MethodDelete, baseURL+"/api/v1/events/"+eventID+"/confirmation", nil, http.StatusOK)
	if reopenedWhole.Status != "confirmed" {
		t.Fatalf("event status after whole reopen = %q, want confirmed groups status", reopenedWhole.Status)
	}
	doJSON[schedulingGroup](t, organizer, http.MethodDelete, baseURL+"/api/v1/events/"+eventID+"/groups/"+group.ID+"/confirmation", nil, http.StatusOK)
	doNoContent(t, organizer, http.MethodDelete, baseURL+"/api/v1/events/"+eventID+"/groups/"+group.ID)
}

func createE2EEvent(t *testing.T, client *http.Client, baseURL, title string, start, end time.Time) event {
	t.Helper()
	return doJSON[event](t, client, http.MethodPost, baseURL+"/api/v1/events", map[string]any{
		"title": title, "time_zone": "Asia/Tokyo", "window_start_at": start,
		"window_end_at": end, "duration_minutes": 30,
	}, http.StatusCreated)
}

func createGuest(t *testing.T, client *http.Client, baseURL, name string) user {
	t.Helper()
	return doJSON[user](t, client, http.MethodPost, baseURL+"/api/v1/guest-sessions", map[string]string{"display_name": name}, http.StatusCreated)
}

func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func doJSON[T any](t *testing.T, client *http.Client, method, url string, body any, wantStatus int) T {
	t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		var errorBody any
		_ = json.NewDecoder(response.Body).Decode(&errorBody)
		t.Fatalf("%s %s status = %d, want %d; body=%v", method, url, response.StatusCode, wantStatus, errorBody)
	}
	var result envelope[T]
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result.Data
}

func doNoContent(t *testing.T, client *http.Client, method, url string) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("%s %s status = %d, want %d", method, url, response.StatusCode, http.StatusNoContent)
	}
}
