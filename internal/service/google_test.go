package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
)

type googleProviderStub struct {
	exchangeIdentity func(context.Context, string, string) (googleclient.Identity, error)
	exchangeCalendar func(context.Context, string) (string, []string, error)
	listCalendars    func(context.Context, string) ([]googleclient.Calendar, error)
	freeBusy         func(context.Context, string, []string, time.Time, time.Time, string) ([]googleclient.BusyInterval, error)
	revoke           func(context.Context, string) error
}

func (s googleProviderStub) LoginAuthURL(state, nonce string) string {
	return "https://accounts.example/login?state=" + state + "&nonce=" + nonce
}
func (s googleProviderStub) ExchangeIdentity(ctx context.Context, code, nonce string) (googleclient.Identity, error) {
	return s.exchangeIdentity(ctx, code, nonce)
}
func (s googleProviderStub) CalendarAuthURL(state string) string {
	return "https://accounts.example/calendar?state=" + state
}
func (s googleProviderStub) ExchangeCalendar(ctx context.Context, code string) (string, []string, error) {
	return s.exchangeCalendar(ctx, code)
}
func (s googleProviderStub) ListCalendars(ctx context.Context, token string) ([]googleclient.Calendar, error) {
	return s.listCalendars(ctx, token)
}
func (s googleProviderStub) FreeBusy(ctx context.Context, token string, ids []string, start, end time.Time, zone string) ([]googleclient.BusyInterval, error) {
	return s.freeBusy(ctx, token, ids, start, end, zone)
}
func (s googleProviderStub) Revoke(ctx context.Context, token string) error {
	return s.revoke(ctx, token)
}

type tokenBoxStub struct{}

func (tokenBoxStub) Seal(value []byte) ([]byte, error) {
	return append([]byte("sealed:"), value...), nil
}
func (tokenBoxStub) Open(value []byte) ([]byte, error) { return value[len("sealed:"):], nil }

func TestCompleteGoogleLoginPromotesCurrentGuest(t *testing.T) {
	store := storeStub{upsertGoogleUser: func(_ context.Context, current, subject, email, name string) (domain.User, error) {
		if current != "guest-1" || subject != "google-sub" || email != "user@example.com" || name != "Google User" {
			t.Fatalf("UpsertGoogleUser arguments = %q, %q, %q, %q", current, subject, email, name)
		}
		return domain.User{ID: "registered-1", AccountType: "registered"}, nil
	}}
	provider := googleProviderStub{exchangeIdentity: func(_ context.Context, code, nonce string) (googleclient.Identity, error) {
		if code != "code" || nonce != "nonce" {
			t.Fatalf("ExchangeIdentity arguments = %q, %q", code, nonce)
		}
		return googleclient.Identity{Subject: "google-sub", Email: "user@example.com", Name: " Google User "}, nil
	}}
	application := New(store, time.Hour, "http://localhost")
	application.ConfigureGoogle(provider, tokenBoxStub{}, 30*24*time.Hour)
	before := time.Now()
	user, expires, err := application.CompleteGoogleLogin(context.Background(), "code", "nonce", "guest-1")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "registered-1" || expires.Before(before.Add(30*24*time.Hour-time.Second)) {
		t.Fatalf("CompleteGoogleLogin() = %#v, %s", user, expires)
	}
}

func TestPreviewGoogleCalendarAvailability(t *testing.T) {
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	end := start.Add(6 * time.Hour)
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{WindowStartAt: start, WindowEndAt: end, TimeZone: "Asia/Tokyo"}, nil
		},
		getGoogleCalendar: func(context.Context, string) ([]byte, []string, error) {
			return []byte("sealed:refresh-token"), []string{"calendar.freebusy"}, nil
		},
	}
	provider := googleProviderStub{freeBusy: func(_ context.Context, token string, ids []string, gotStart, gotEnd time.Time, zone string) ([]googleclient.BusyInterval, error) {
		if token != "refresh-token" || !reflect.DeepEqual(ids, []string{"primary"}) || !gotStart.Equal(start) || !gotEnd.Equal(end) || zone != "Asia/Tokyo" {
			t.Fatalf("FreeBusy arguments = %q, %#v, %s, %s, %q", token, ids, gotStart, gotEnd, zone)
		}
		return []googleclient.BusyInterval{
			{StartAt: start.Add(2 * time.Hour), EndAt: start.Add(4 * time.Hour)},
			{StartAt: start.Add(3 * time.Hour), EndAt: start.Add(5 * time.Hour)},
		}, nil
	}}
	application := New(store, time.Hour, "http://localhost")
	application.ConfigureGoogle(provider, tokenBoxStub{}, time.Hour)
	intervals, err := application.PreviewGoogleCalendarAvailability(context.Background(), "event", "user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(intervals) != 2 || !intervals[0].StartAt.Equal(start) || !intervals[0].EndAt.Equal(start.Add(2*time.Hour)) || !intervals[1].StartAt.Equal(start.Add(5*time.Hour)) || !intervals[1].EndAt.Equal(end) {
		t.Fatalf("intervals = %#v", intervals)
	}
}

func TestListGoogleCalendars(t *testing.T) {
	store := storeStub{
		getGoogleCalendar: func(context.Context, string) ([]byte, []string, error) {
			return []byte("sealed:refresh-token"), []string{"calendar.freebusy", "calendar.calendarlist.readonly"}, nil
		},
	}
	provider := googleProviderStub{listCalendars: func(_ context.Context, token string) ([]googleclient.Calendar, error) {
		if token != "refresh-token" {
			t.Fatalf("ListCalendars token = %q", token)
		}
		return []googleclient.Calendar{{ID: "primary", Summary: "Main", Primary: true}}, nil
	}}
	application := New(store, time.Hour, "http://localhost")
	application.ConfigureGoogle(provider, tokenBoxStub{}, time.Hour)
	calendars, err := application.ListGoogleCalendars(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(calendars) != 1 || calendars[0].ID != "primary" || !calendars[0].Primary {
		t.Fatalf("calendars = %#v", calendars)
	}
}

func TestGoogleMethodsFailWhenNotConfigured(t *testing.T) {
	application := New(storeStub{}, time.Hour, "http://localhost")
	if _, err := application.GoogleLoginURL("state", "nonce"); !errors.Is(err, domain.ErrGoogleNotConfigured) {
		t.Fatalf("GoogleLoginURL() error = %v", err)
	}
}

func TestDisconnectGoogleCalendarKeepsConnectionWhenRevokeFails(t *testing.T) {
	deleted := false
	store := storeStub{
		getGoogleCalendar: func(context.Context, string) ([]byte, []string, error) {
			return []byte("sealed:refresh-token"), nil, nil
		},
		deleteGoogleCalendar: func(context.Context, string) ([]byte, error) {
			deleted = true
			return []byte("sealed:refresh-token"), nil
		},
	}
	provider := googleProviderStub{revoke: func(context.Context, string) error { return errors.New("temporary failure") }}
	application := New(store, time.Hour, "http://localhost")
	application.ConfigureGoogle(provider, tokenBoxStub{}, time.Hour)
	if err := application.DisconnectGoogleCalendar(context.Background(), "user"); err == nil {
		t.Fatal("DisconnectGoogleCalendar() error = nil")
	}
	if deleted {
		t.Fatal("connection was deleted before remote revocation succeeded")
	}
}
