package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
)

func confirmedCalendarTestEvent() domain.Event {
	startAt := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	endAt := startAt.Add(time.Hour)
	return domain.Event{
		ID: "event-1", Title: "定例会",
		SchedulingMode: "whole_event", Status: "confirmed",
		ConfirmedStartAt: &startAt, ConfirmedEndAt: &endAt,
	}
}

func TestSyncOwnConfirmationCalendarRetriesAndStoresSuccess(t *testing.T) {
	event := confirmedCalendarTestEvent()
	var storedStatus string
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return event, nil
		},
		getGoogleCalendar: func(context.Context, string) ([]byte, []string, error) {
			return []byte("sealed:refresh-token"), []string{googleclient.CalendarEventsScope}, nil
		},
		upsertCalendarSync: func(_ context.Context, eventID string, groupID *string, userID, status string) (domain.ConfirmationCalendarSync, error) {
			storedStatus = status
			return domain.ConfirmationCalendarSync{EventID: eventID, GroupID: groupID, UserID: userID, Status: status}, nil
		},
	}
	var gotEvent googleclient.CalendarEvent
	provider := googleProviderStub{upsertEvent: func(_ context.Context, token string, entry googleclient.CalendarEvent) error {
		if token != "refresh-token" {
			t.Fatalf("token = %q", token)
		}
		gotEvent = entry
		return nil
	}}
	application := New(store, time.Hour, "https://vacaro.example")
	application.ConfigureGoogle(provider, tokenBoxStub{}, time.Hour)

	result, err := application.SyncOwnConfirmationCalendar(context.Background(), event.ID, nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "synced" || storedStatus != "synced" {
		t.Fatalf("sync = %#v, stored status = %q", result, storedStatus)
	}
	if gotEvent.Summary != event.Title || !gotEvent.StartAt.Equal(*event.ConfirmedStartAt) {
		t.Fatalf("calendar event = %#v", gotEvent)
	}
}

func TestSyncOwnConfirmationCalendarStoresProviderFailure(t *testing.T) {
	event := confirmedCalendarTestEvent()
	var storedStatus string
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return event, nil
		},
		getGoogleCalendar: func(context.Context, string) ([]byte, []string, error) {
			return []byte("sealed:refresh-token"), []string{googleclient.CalendarEventsScope}, nil
		},
		upsertCalendarSync: func(_ context.Context, _ string, _ *string, _, status string) (domain.ConfirmationCalendarSync, error) {
			storedStatus = status
			return domain.ConfirmationCalendarSync{Status: status}, nil
		},
	}
	provider := googleProviderStub{upsertEvent: func(context.Context, string, googleclient.CalendarEvent) error {
		return errors.New("provider unavailable")
	}}
	application := New(store, time.Hour, "")
	application.ConfigureGoogle(provider, tokenBoxStub{}, time.Hour)

	_, err := application.SyncOwnConfirmationCalendar(context.Background(), event.ID, nil, "member")
	if !errors.Is(err, domain.ErrCalendarSyncFailed) {
		t.Fatalf("error = %v", err)
	}
	if storedStatus != "failed" {
		t.Fatalf("stored status = %q", storedStatus)
	}
}

func TestConfirmEventRecordsAutomaticCalendarFailureWithoutRollingBackConfirmation(t *testing.T) {
	event := confirmedCalendarTestEvent()
	event.Status = "open"
	event.WindowStartAt = event.ConfirmedStartAt.Add(-time.Hour)
	event.WindowEndAt = event.ConfirmedEndAt.Add(time.Hour)
	event.DurationMinutes = 60
	event.CreatedByUserID = "owner"
	var storedStatus string
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
			return []domain.Participant{{UserID: "member"}}, nil
		},
		getGoogleCalendar: func(context.Context, string) ([]byte, []string, error) {
			return []byte("sealed:refresh-token"), []string{googleclient.CalendarEventsScope}, nil
		},
		upsertCalendarSync: func(_ context.Context, _ string, _ *string, _ string, status string) (domain.ConfirmationCalendarSync, error) {
			storedStatus = status
			return domain.ConfirmationCalendarSync{Status: status}, nil
		},
	}
	provider := googleProviderStub{upsertEvent: func(context.Context, string, googleclient.CalendarEvent) error {
		return errors.New("provider unavailable")
	}}
	application := New(store, time.Hour, "")
	application.ConfigureGoogle(provider, tokenBoxStub{}, time.Hour)

	confirmed, err := application.ConfirmEvent(
		context.Background(), event.ID, "owner", *event.ConfirmedStartAt, *event.ConfirmedEndAt,
	)
	if err != nil || confirmed.Status != "confirmed" {
		t.Fatalf("ConfirmEvent() = %#v, %v", confirmed, err)
	}
	if storedStatus != "failed" {
		t.Fatalf("stored status = %q", storedStatus)
	}
}

func TestSyncOwnGroupCalendarRequiresMembership(t *testing.T) {
	event := confirmedCalendarTestEvent()
	event.SchedulingMode = "groups"
	groupID := "group-1"
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return event, nil
		},
		getGroup: func(context.Context, string, string, string) (domain.SchedulingGroup, error) {
			return domain.SchedulingGroup{
				ID: groupID, ConfirmedStartAt: event.ConfirmedStartAt, ConfirmedEndAt: event.ConfirmedEndAt,
				Members: []domain.Participant{{UserID: "another-user"}},
			}, nil
		},
	}
	application := New(store, time.Hour, "")
	application.ConfigureGoogle(googleProviderStub{}, tokenBoxStub{}, time.Hour)

	_, err := application.SyncOwnConfirmationCalendar(context.Background(), event.ID, &groupID, "member")
	if !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("error = %v", err)
	}
}

func TestGetConfirmationCalendarStatusReportsMissingWritePermission(t *testing.T) {
	event := confirmedCalendarTestEvent()
	attemptedAt := time.Now().UTC()
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return event, nil
		},
		listCalendarSyncs: func(context.Context, string, string) ([]domain.ConfirmationCalendarSync, error) {
			return []domain.ConfirmationCalendarSync{{Status: "failed", AttemptedAt: &attemptedAt}}, nil
		},
		getGoogleCalendar: func(context.Context, string) ([]byte, []string, error) {
			return []byte("sealed:refresh-token"), []string{"calendar.freebusy"}, nil
		},
	}
	application := New(store, time.Hour, "")
	application.ConfigureGoogle(googleProviderStub{}, tokenBoxStub{}, time.Hour)

	status, err := application.GetConfirmationCalendarStatus(context.Background(), event.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	if status.ConnectionStatus != "write_permission_required" || len(status.Syncs) != 1 {
		t.Fatalf("status = %#v", status)
	}
}
