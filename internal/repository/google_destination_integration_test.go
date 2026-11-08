package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDedicatedCalendarDestinationIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	user, err := store.UpsertGoogleUser(ctx, "", "destination-"+randomHash(t), "dest@example.com", "Calendar Owner")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID) }()
	var creates atomic.Int32
	resolve := func(current string) (string, error) {
		if current == "" {
			creates.Add(1)
			time.Sleep(15 * time.Millisecond)
			return "dedicated-calendar", nil
		}
		if current != "dedicated-calendar" {
			return "", errors.New("unexpected destination")
		}
		return current, nil
	}
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			id, err := store.ResolveGoogleCalendarDestination(ctx, user.ID, resolve)
			if err == nil && id != "dedicated-calendar" {
				err = errors.New("incorrect destination")
			}
			failures <- err
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if creates.Load() != 1 {
		t.Fatalf("concurrent provisioning created %d calendars", creates.Load())
	}
	if err := store.SaveGoogleCalendarConnection(ctx, user.ID, []byte("encrypted"), []string{"test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteGoogleCalendarConnection(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if id, err := store.GetGoogleCalendarDestination(ctx, user.ID); err != nil || id != "dedicated-calendar" {
		t.Fatalf("disconnect lost destination: %q,%v", id, err)
	}
	if _, err := store.ResolveGoogleCalendarDestination(ctx, user.ID, func(string) (string, error) { return "primary", nil }); err == nil {
		t.Fatal("primary destination accepted")
	}
	if id, _ := store.GetGoogleCalendarDestination(ctx, user.ID); id != "dedicated-calendar" {
		t.Fatal("invalid destination was committed")
	}

	start := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	event, err := store.CreateEvent(ctx, CreateEventParams{CreatedByUserID: user.ID, InviteTokenHash: randomHash(t), Title: "Dedicated test", WindowStartAt: start, WindowEndAt: start.Add(time.Hour), DurationMinutes: 30, OrganizerName: "Owner"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM events WHERE id=$1`, event.ID) }()
	// Pre-migration primary syncs have no destination and are never shown as
	// successfully synced to the dedicated calendar.
	if _, err := store.UpsertConfirmationCalendarSync(ctx, event.ID, nil, user.ID, "", "synced"); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListConfirmationCalendarSyncs(ctx, event.ID, user.ID); err != nil || len(rows) != 0 {
		t.Fatalf("legacy sync was shown: %v,%v", rows, err)
	}
	if _, err := store.UpsertConfirmationCalendarSync(ctx, event.ID, nil, user.ID, "dedicated-calendar", "synced"); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListConfirmationCalendarSyncs(ctx, event.ID, user.ID); err != nil || len(rows) != 1 {
		t.Fatalf("dedicated sync missing: %v,%v", rows, err)
	}
	if _, err := store.ResolveGoogleCalendarDestination(ctx, user.ID, func(string) (string, error) { return "replacement-calendar", nil }); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.ListConfirmationCalendarSyncs(ctx, event.ID, user.ID); err != nil || len(rows) != 0 {
		t.Fatalf("old destination still shown as synced: %v,%v", rows, err)
	}
}
