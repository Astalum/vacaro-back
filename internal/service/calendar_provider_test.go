package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/astalum/vacaro-back/internal/icloud"
)

func TestOnlySelectedCalendarIsUsed(t *testing.T) {
	for _, selected := range []string{"none", "google", "icloud", "timetree"} {
		for _, transport := range []string{"google", "icloud"} {
			for _, importing := range []bool{false, true} {
				app := New(storeStub{calendarProvider: selected}, time.Hour, "")
				called := false
				err := app.withActiveCalendar(context.Background(), "user", transport, importing, func() error { called = true; return nil })
				allowed := (selected == transport || (selected == "timetree" && transport == "google")) && !(selected == "timetree" && importing)
				if called != allowed || (!allowed && !errors.Is(err, domain.ErrInactiveCalendar)) || (allowed && err != nil) {
					t.Fatalf("selected=%s transport=%s importing=%v called=%v err=%v", selected, transport, importing, called, err)
				}
			}
		}
	}
}
func TestInactiveEndpointsNeverReachProviders(t *testing.T) {
	app := New(storeStub{calendarProvider: "none"}, time.Hour, "")
	calls := []func() error{
		func() error { return app.CompleteGoogleCalendar(context.Background(), "user", "code", "verifier") },
		func() error { _, err := app.ListGoogleCalendars(context.Background(), "user"); return err },
		func() error {
			_, err := app.PreviewGoogleCalendarAvailability(context.Background(), "event", "user", nil)
			return err
		},
		func() error {
			return app.ConnectICloud(context.Background(), "user", "apple@example.com", "aaaa-bbbb-cccc-dddd")
		},
		func() error { _, err := app.ListICloudCalendars(context.Background(), "user"); return err },
		func() error {
			_, err := app.PreviewICloudAvailability(context.Background(), "event", "user", nil)
			return err
		},
	}
	for i, call := range calls {
		if err := call(); !errors.Is(err, domain.ErrInactiveCalendar) {
			t.Fatalf("operation %d error=%v", i, err)
		}
	}
}
func TestCalendarSelectionValidatesBeforeSaving(t *testing.T) {
	saves := 0
	app := New(storeStub{setCalendarProvider: func(_ context.Context, _ string, provider string) error {
		saves++
		if provider != "icloud" {
			t.Fatal(provider)
		}
		return nil
	}}, time.Hour, "")
	for _, invalid := range []string{"", "google,icloud", "all", "Google"} {
		if _, err := app.SetCalendarPreference(context.Background(), "user", invalid); err == nil {
			t.Fatal("invalid provider accepted")
		}
	}
	if saves != 0 {
		t.Fatal("invalid selection persisted")
	}
	got, err := app.SetCalendarPreference(context.Background(), "user", "icloud")
	if err != nil || got.Provider != "icloud" || saves != 1 {
		t.Fatalf("got=%v err=%v saves=%d", got, err, saves)
	}
}

func TestAutomaticConfirmationUsesOnlySelectedProviderWithBothConnections(t *testing.T) {
	for _, selected := range []string{"none", "google", "icloud", "timetree"} {
		t.Run(selected, func(t *testing.T) {
			googleWrites, icloudWrites := 0, 0
			store := storeStub{
				calendarProvider: selected,
				getGoogleCalendar: func(context.Context, string) ([]byte, []string, error) {
					return []byte("sealed:refresh-token"), []string{googleclient.CalendarAppCreatedScope}, nil
				},
				getICloud: storedICloud,
			}
			app := New(store, time.Hour, "")
			app.ConfigureGoogle(googleProviderStub{upsertEvent: func(context.Context, string, googleclient.CalendarEvent) error { googleWrites++; return nil }}, tokenBoxStub{}, time.Hour)
			app.ConfigureICloud(icloudProviderStub{upsert: func(context.Context, icloud.Credentials, googleclient.CalendarEvent) (bool, error) {
				icloudWrites++
				return false, nil
			}}, tokenBoxStub{})
			app.syncSelectedCalendars(context.Background(), []domain.Participant{{UserID: "member"}, {UserID: "member"}}, googleclient.CalendarEvent{}, "event", "")
			wantGoogle, wantICloud := 0, 0
			if selected == "google" || selected == "timetree" {
				wantGoogle = 1
			}
			if selected == "icloud" {
				wantICloud = 1
			}
			if googleWrites != wantGoogle || icloudWrites != wantICloud {
				t.Fatalf("google=%d icloud=%d", googleWrites, icloudWrites)
			}
		})
	}
}
