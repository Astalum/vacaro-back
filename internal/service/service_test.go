package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/astalum/vacaro-back/internal/repository"
)

func TestCreateGuestTrimsNameAndSetsExpiration(t *testing.T) {
	before := time.Now().UTC()
	var gotName string
	var gotExpiration time.Time
	store := storeStub{createGuest: func(_ context.Context, name string, expiration time.Time) (domain.User, error) {
		gotName, gotExpiration = name, expiration
		return domain.User{DisplayName: name, GuestExpiresAt: &expiration}, nil
	}}
	service := New(store, 24*time.Hour, "http://localhost:3000")
	if _, err := service.CreateGuest(context.Background(), "  Tanaka  "); err != nil {
		t.Fatal(err)
	}
	if gotName != "Tanaka" {
		t.Errorf("name = %q, want Tanaka", gotName)
	}
	wantExpiration := before.Add(24 * time.Hour)
	if gotExpiration.Before(wantExpiration) || gotExpiration.After(wantExpiration.Add(time.Second)) {
		t.Errorf("expiration = %s, want around %s", gotExpiration, wantExpiration)
	}
}

func TestCreateGuestRejectsInvalidName(t *testing.T) {
	service := New(storeStub{}, time.Hour, "")
	if _, err := service.CreateGuest(context.Background(), "   "); err == nil {
		t.Fatal("CreateGuest() error = nil, want validation error")
	}
}

func TestCreateEvent(t *testing.T) {
	base := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	var got repository.CreateEventParams
	store := storeStub{
		getUser: func(context.Context, string) (domain.User, error) {
			return domain.User{ID: "user-id", DisplayName: "Organizer"}, nil
		},
		createEvent: func(_ context.Context, params repository.CreateEventParams) (domain.Event, error) {
			got = params
			return domain.Event{ID: "event-id", Title: params.Title}, nil
		},
	}
	service := New(store, time.Hour, "http://localhost:3000/")
	result, err := service.CreateEvent(context.Background(), "user-id", CreateEventInput{
		Title: " Event ", TimeZone: "Asia/Tokyo", WindowStartAt: base,
		WindowEndAt: base.Add(2 * time.Hour), DurationMinutes: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Event" || got.OrganizerName != "Organizer" || len(got.InviteTokenHash) != 64 {
		t.Errorf("params = %#v", got)
	}
	if result.InviteToken == "" || result.InviteURL != "http://localhost:3000/invitations/"+result.InviteToken {
		t.Errorf("invite result = %#v", result)
	}
	if hashToken(result.InviteToken) != got.InviteTokenHash {
		t.Error("stored hash does not match invite token")
	}
}

func TestValidateEvent(t *testing.T) {
	base := time.Now().UTC()
	tests := []struct {
		name  string
		input CreateEventInput
	}{
		{name: "blank title", input: CreateEventInput{TimeZone: "Asia/Tokyo", WindowStartAt: base, WindowEndAt: base.Add(time.Hour), DurationMinutes: 60}},
		{name: "invalid timezone", input: CreateEventInput{Title: "Event", TimeZone: "Mars/Olympus", WindowStartAt: base, WindowEndAt: base.Add(time.Hour), DurationMinutes: 60}},
		{name: "invalid window", input: CreateEventInput{Title: "Event", TimeZone: "UTC", WindowStartAt: base, WindowEndAt: base, DurationMinutes: 60}},
		{name: "short duration", input: CreateEventInput{Title: "Event", TimeZone: "UTC", WindowStartAt: base, WindowEndAt: base.Add(time.Hour), DurationMinutes: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateEvent(tt.input); err == nil {
				t.Fatal("validateEvent() error = nil")
			}
		})
	}
}

func TestListEventsPagination(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	call := 0
	store := storeStub{listEvents: func(_ context.Context, _, _ string, cursorTime *time.Time, cursorID *string, limit int) ([]domain.Event, error) {
		call++
		if limit != 2 {
			t.Errorf("store limit = %d, want 2", limit)
		}
		if call == 2 && (cursorTime == nil || cursorID == nil || *cursorID != "event-1") {
			t.Errorf("decoded cursor = %v, %v", cursorTime, cursorID)
		}
		return []domain.Event{{ID: "event-1", CreatedAt: base}, {ID: "event-2", CreatedAt: base.Add(-time.Minute)}}, nil
	}}
	service := New(store, time.Hour, "")
	first, err := service.ListEvents(context.Background(), "user-id", "open", "", 1)
	if err != nil || first.NextCursor == nil || len(first.Events) != 1 {
		t.Fatalf("first page = %#v, err=%v", first, err)
	}
	if _, err := service.ListEvents(context.Background(), "user-id", "open", *first.NextCursor, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListEvents(context.Background(), "user-id", "invalid", "", 1); err == nil {
		t.Fatal("invalid status error = nil")
	}
	if _, err := service.ListEvents(context.Background(), "user-id", "", "invalid-cursor", 1); err == nil {
		t.Fatal("invalid cursor error = nil")
	}
}

func TestReplaceAvailabilityMergesAdjacentIntervals(t *testing.T) {
	base := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	var stored []domain.AvailabilityInterval
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{Status: "open", WindowStartAt: base, WindowEndAt: base.Add(4 * time.Hour)}, nil
		},
		replaceAvailability: func(_ context.Context, _, _ string, intervals []domain.AvailabilityInterval) ([]domain.AvailabilityInterval, error) {
			stored = intervals
			return intervals, nil
		},
	}
	service := New(store, time.Hour, "")
	_, err := service.ReplaceAvailability(context.Background(), "event", "user", []IntervalInput{
		{StartAt: base.Add(time.Hour), EndAt: base.Add(2 * time.Hour)},
		{StartAt: base, EndAt: base.Add(time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || !stored[0].StartAt.Equal(base) || !stored[0].EndAt.Equal(base.Add(2*time.Hour)) {
		t.Fatalf("stored intervals = %#v", stored)
	}
}

func TestReplaceAvailabilityRejectsInvalidIntervals(t *testing.T) {
	base := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	store := storeStub{getEventForUser: func(context.Context, string, string) (domain.Event, error) {
		return domain.Event{Status: "open", WindowStartAt: base, WindowEndAt: base.Add(4 * time.Hour)}, nil
	}}
	service := New(store, time.Hour, "")
	tests := [][]IntervalInput{
		{{StartAt: base, EndAt: base}},
		{{StartAt: base, EndAt: base.Add(2 * time.Hour)}, {StartAt: base.Add(time.Hour), EndAt: base.Add(3 * time.Hour)}},
		{{StartAt: base.Add(-time.Minute), EndAt: base.Add(time.Hour)}},
	}
	for _, intervals := range tests {
		if _, err := service.ReplaceAvailability(context.Background(), "event", "user", intervals); err == nil {
			t.Errorf("ReplaceAvailability(%#v) error = nil", intervals)
		}
	}
}

func TestConfirmEventValidation(t *testing.T) {
	base := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	store := storeStub{getEventForUser: func(context.Context, string, string) (domain.Event, error) {
		return domain.Event{CreatedByUserID: "owner", WindowStartAt: base, WindowEndAt: base.Add(4 * time.Hour), DurationMinutes: 60}, nil
	}}
	service := New(store, time.Hour, "")
	if _, err := service.ConfirmEvent(context.Background(), "event", "other", base, base.Add(time.Hour)); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("permission error = %v", err)
	}
	if _, err := service.ConfirmEvent(context.Background(), "event", "owner", base, base.Add(30*time.Minute)); err == nil {
		t.Fatal("duration validation error = nil")
	}
}

func TestConfirmEventSyncsConnectedParticipantCalendar(t *testing.T) {
	base := time.Date(2026, 10, 10, 1, 15, 0, 0, time.UTC)
	event := domain.Event{
		ID: "event-1", CreatedByUserID: "owner", Title: "定例会", TimeZone: "Asia/Tokyo",
		WindowStartAt: base, WindowEndAt: base.Add(4 * time.Hour), DurationMinutes: 60,
	}
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return event, nil
		},
		confirmEvent: func(context.Context, string, string, time.Time, time.Time) (domain.Event, error) {
			confirmed := event
			confirmed.Status = "confirmed"
			return confirmed, nil
		},
		listParticipants: func(context.Context, string, string) ([]domain.Participant, error) {
			return []domain.Participant{
				{EventID: event.ID, UserID: "connected-user"},
				{EventID: event.ID, UserID: "legacy-user"},
			}, nil
		},
		getGoogleCalendar: func(_ context.Context, userID string) ([]byte, []string, error) {
			if userID == "connected-user" {
				return []byte("sealed:refresh-token"), []string{googleclient.CalendarEventsScope}, nil
			}
			return []byte("sealed:legacy-token"), []string{"https://www.googleapis.com/auth/calendar.freebusy"}, nil
		},
	}
	var gotToken string
	var gotEvent googleclient.CalendarEvent
	provider := googleProviderStub{upsertEvent: func(_ context.Context, token string, entry googleclient.CalendarEvent) error {
		gotToken, gotEvent = token, entry
		return nil
	}}
	application := New(store, time.Hour, "https://vacaro.example")
	application.ConfigureGoogle(provider, tokenBoxStub{}, time.Hour)

	if _, err := application.ConfirmEvent(context.Background(), event.ID, "owner", base, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if gotToken != "refresh-token" || gotEvent.Summary != event.Title || gotEvent.TimeZone != event.TimeZone {
		t.Fatalf("calendar sync = token %q, event %#v", gotToken, gotEvent)
	}
	if gotEvent.Description != "vacaroで確定した予定です。\nhttps://vacaro.example/events/event-1/confirmed" {
		t.Fatalf("description = %q", gotEvent.Description)
	}
	if gotEvent.ID != confirmationCalendarEventID(event.ID, "") {
		t.Fatalf("calendar event ID = %q", gotEvent.ID)
	}
}
