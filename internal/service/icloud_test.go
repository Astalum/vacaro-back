package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/astalum/vacaro-back/internal/icloud"
)

type icloudProviderStub struct {
	discover func(context.Context, string, string, string) (icloud.Credentials, error)
	list     func(context.Context, icloud.Credentials) ([]icloud.Calendar, error)
	busy     func(context.Context, icloud.Credentials, []string, time.Time, time.Time, string) ([]googleclient.BusyInterval, error)
	upsert   func(context.Context, icloud.Credentials, googleclient.CalendarEvent) (bool, error)
	delete   func(context.Context, icloud.Credentials, string) error
}

func (s icloudProviderStub) Discover(ctx context.Context, a, b, id string) (icloud.Credentials, error) {
	return s.discover(ctx, a, b, id)
}
func (s icloudProviderStub) ListCalendars(ctx context.Context, c icloud.Credentials) ([]icloud.Calendar, error) {
	return s.list(ctx, c)
}
func (s icloudProviderStub) Busy(ctx context.Context, c icloud.Credentials, ids []string, a, b time.Time, z string) ([]googleclient.BusyInterval, error) {
	return s.busy(ctx, c, ids, a, b, z)
}
func (s icloudProviderStub) UpsertEvent(ctx context.Context, c icloud.Credentials, e googleclient.CalendarEvent) (bool, error) {
	return s.upsert(ctx, c, e)
}
func (s icloudProviderStub) DeleteEvent(ctx context.Context, c icloud.Credentials, id string) error {
	return s.delete(ctx, c, id)
}
func testICloudCredentials() icloud.Credentials {
	return icloud.Credentials{Username: "apple@example.com", Password: "aaaa-bbbb-cccc-dddd", HomeURL: "https://caldav.icloud.com/home/", CalendarURL: "https://caldav.icloud.com/home/vacaro/"}
}
func storedICloud(context.Context, string) ([]byte, string, error) {
	data, _ := json.Marshal(testICloudCredentials())
	encrypted, _ := tokenBoxStub{}.Seal(data)
	return encrypted, testICloudCredentials().CalendarURL, nil
}
func TestConnectICloudValidatesAndEncrypts(t *testing.T) {
	called := false
	saved := false
	store := storeStub{saveICloud: func(_ context.Context, id string, cipher []byte, dest string) error {
		saved = true
		if id != "user" || !strings.HasPrefix(string(cipher), "sealed:") || dest != testICloudCredentials().CalendarURL {
			t.Fatal("unsealed or incorrect connection")
		}
		return nil
	}}
	provider := icloudProviderStub{discover: func(_ context.Context, a, b, id string) (icloud.Credentials, error) {
		called = true
		if a != "apple@example.com" || b != "aaaa-bbbb-cccc-dddd" || id != "user" {
			t.Fatal("incorrect credentials")
		}
		return testICloudCredentials(), nil
	}}
	app := New(store, time.Hour, "")
	app.ConfigureICloud(provider, tokenBoxStub{})
	for _, password := range []string{"", "usual-password", "abcd-abcd-abcd-1234"} {
		if err := app.ConnectICloud(context.Background(), "user", "apple@example.com", password); err == nil {
			t.Fatal("invalid app password accepted")
		}
	}
	if called || saved {
		t.Fatal("invalid credentials reached provider or storage")
	}
	if err := app.ConnectICloud(context.Background(), "user", "apple@example.com", "aaaa-bbbb-cccc-dddd"); err != nil {
		t.Fatal(err)
	}
	if !called || !saved {
		t.Fatal("connection not persisted")
	}
}
func TestICloudPreviewUsesBusyTimesAndFailsClosed(t *testing.T) {
	start := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	store := storeStub{getICloud: storedICloud, getEventForUser: func(context.Context, string, string) (domain.Event, error) {
		return domain.Event{WindowStartAt: start, WindowEndAt: start.Add(4 * time.Hour)}, nil
	}}
	provider := icloudProviderStub{busy: func(_ context.Context, c icloud.Credentials, ids []string, a, b time.Time, zone string) ([]googleclient.BusyInterval, error) {
		if c.Password != testICloudCredentials().Password || zone != "Asia/Tokyo" || len(ids) != 1 {
			t.Fatal("incorrect import inputs")
		}
		return []googleclient.BusyInterval{{StartAt: start.Add(time.Hour), EndAt: start.Add(2 * time.Hour)}}, nil
	}}
	app := New(store, time.Hour, "")
	app.ConfigureICloud(provider, tokenBoxStub{})
	free, err := app.PreviewICloudAvailability(context.Background(), "event", "user", []string{"calendar"})
	if err != nil || len(free) != 2 || free[0].Source != "icloud_calendar" || !free[1].StartAt.Equal(start.Add(2*time.Hour)) {
		t.Fatalf("preview: %v %v", free, err)
	}
	provider.busy = func(context.Context, icloud.Credentials, []string, time.Time, time.Time, string) ([]googleclient.BusyInterval, error) {
		return []googleclient.BusyInterval{{StartAt: start, EndAt: start.Add(time.Hour)}}, &icloud.Error{Kind: "recurrence_not_expanded"}
	}
	app.ConfigureICloud(provider, tokenBoxStub{})
	if partial, err := app.PreviewICloudAvailability(context.Background(), "event", "user", nil); err == nil || partial != nil {
		t.Fatalf("partial calendar treated as free: %v %v", partial, err)
	}
}
func TestICloudConfirmationRetryAndPermission(t *testing.T) {
	event := confirmedCalendarTestEvent()
	groupID := "group"
	store := storeStub{getICloud: storedICloud, getEventForUser: func(context.Context, string, string) (domain.Event, error) { return event, nil }, getGroup: func(context.Context, string, string, string) (domain.SchedulingGroup, error) {
		return domain.SchedulingGroup{ID: groupID, ConfirmedStartAt: event.ConfirmedStartAt, ConfirmedEndAt: event.ConfirmedEndAt, Members: []domain.Participant{{UserID: "other"}}}, nil
	}}
	wrote := false
	status := ""
	store.upsertICloudSync = func(_ context.Context, _ string, _ *string, _, _, s string) (domain.ConfirmationCalendarSync, error) {
		status = s
		return domain.ConfirmationCalendarSync{Status: s}, nil
	}
	provider := icloudProviderStub{upsert: func(_ context.Context, c icloud.Credentials, e googleclient.CalendarEvent) (bool, error) {
		wrote = true
		if c.CalendarURL != testICloudCredentials().CalendarURL || e.ID != confirmationCalendarEventID(event.ID, "") {
			t.Fatal("incorrect destination")
		}
		return false, nil
	}}
	app := New(store, time.Hour, "")
	app.ConfigureICloud(provider, tokenBoxStub{})
	result, err := app.SyncOwnICloudCalendar(context.Background(), event.ID, nil, "member")
	if err != nil || result.Status != "synced" || !wrote || status != "synced" {
		t.Fatalf("sync: %v %v", result, err)
	}
	event.SchedulingMode = "groups"
	wrote = false
	if _, err := app.SyncOwnICloudCalendar(context.Background(), event.ID, &groupID, "member"); !errors.Is(err, domain.ErrPermissionDenied) || wrote {
		t.Fatalf("nonmember wrote a group: %v", err)
	}
	provider.upsert = func(context.Context, icloud.Credentials, googleclient.CalendarEvent) (bool, error) {
		return false, &icloud.Error{Kind: "reconnect_required", Status: 401}
	}
	app.ConfigureICloud(provider, tokenBoxStub{})
	if _, err := app.SyncOwnICloudCalendar(context.Background(), event.ID, nil, "member"); !errors.Is(err, domain.ErrICloudSyncFailed) || status != "failed" {
		t.Fatalf("failure not retained: %v %s", err, status)
	}
}
func TestICloudDisconnectDoesNotDeleteCalendar(t *testing.T) {
	deleted := false
	app := New(storeStub{deleteICloud: func(context.Context, string) error { deleted = true; return nil }}, time.Hour, "")
	app.ConfigureICloud(icloudProviderStub{}, tokenBoxStub{})
	if err := app.DisconnectICloud(context.Background(), "user"); err != nil || !deleted {
		t.Fatal(err)
	}
}

func TestICloudAutomaticSyncFailureDoesNotRollBackConfirmation(t *testing.T) {
	event := confirmedCalendarTestEvent()
	event.Status = "open"
	event.CreatedByUserID = "owner"
	event.WindowStartAt = event.ConfirmedStartAt.Add(-time.Hour)
	event.WindowEndAt = event.ConfirmedEndAt.Add(time.Hour)
	event.DurationMinutes = 60
	status := ""
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) { return event, nil },
		confirmEvent: func(context.Context, string, string, time.Time, time.Time) (domain.Event, error) {
			result := event
			result.Status = "confirmed"
			return result, nil
		},
		listParticipants: func(context.Context, string, string) ([]domain.Participant, error) {
			return []domain.Participant{{UserID: "member"}}, nil
		}, getICloud: storedICloud,
		upsertICloudSync: func(_ context.Context, _ string, _ *string, _, _, s string) (domain.ConfirmationCalendarSync, error) {
			status = s
			return domain.ConfirmationCalendarSync{Status: s}, nil
		},
	}
	app := New(store, time.Hour, "")
	app.ConfigureICloud(icloudProviderStub{upsert: func(context.Context, icloud.Credentials, googleclient.CalendarEvent) (bool, error) {
		return false, &icloud.Error{Kind: "temporarily_unavailable"}
	}}, tokenBoxStub{})
	result, err := app.ConfirmEvent(context.Background(), event.ID, "owner", *event.ConfirmedStartAt, *event.ConfirmedEndAt)
	if err != nil || result.Status != "confirmed" || status != "failed" {
		t.Fatalf("confirmation: %v %v status=%s", result, err, status)
	}
}

func TestRecreatedICloudCalendarInvalidatesOldSyncSuccess(t *testing.T) {
	event := confirmedCalendarTestEvent()
	cleared := false
	store := storeStub{getEventForUser: func(context.Context, string, string) (domain.Event, error) { return event, nil }, getICloud: storedICloud, clearAllICloudSyncs: func(context.Context, string) error { cleared = true; return nil }}
	app := New(store, time.Hour, "")
	app.ConfigureICloud(icloudProviderStub{upsert: func(context.Context, icloud.Credentials, googleclient.CalendarEvent) (bool, error) { return true, nil }}, tokenBoxStub{})
	if _, err := app.SyncOwnICloudCalendar(context.Background(), event.ID, nil, "member"); err != nil || !cleared {
		t.Fatalf("stale sync successes retained: %v", err)
	}
}

func TestCancelEventDeletesOwnAndGroupICloudEvents(t *testing.T) {
	event := confirmedCalendarTestEvent()
	event.SchedulingMode = "groups"
	deleted := map[string]int{}
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) { return event, nil },
		listGroups: func(context.Context, string, string) ([]domain.SchedulingGroup, error) {
			return []domain.SchedulingGroup{{ID: "group", Members: []domain.Participant{{UserID: "member"}}}}, nil
		},
		cancelEvent: func(context.Context, string, string) error { return nil },
		listParticipants: func(context.Context, string, string) ([]domain.Participant, error) {
			return []domain.Participant{{UserID: "member"}}, nil
		}, getICloud: storedICloud,
	}
	app := New(store, time.Hour, "")
	app.ConfigureICloud(icloudProviderStub{delete: func(_ context.Context, c icloud.Credentials, id string) error {
		if c.CalendarURL != testICloudCredentials().CalendarURL {
			t.Fatal("wrong calendar")
		}
		deleted[id]++
		return nil
	}}, tokenBoxStub{})
	if err := app.CancelEvent(context.Background(), event.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{confirmationCalendarEventID(event.ID, ""), confirmationCalendarEventID(event.ID, "group")} {
		if deleted[id] != 1 {
			t.Fatalf("missing deletion for %s", id)
		}
	}
}
