package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCalendarSelectionSerializesWithProviderOperations(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	user, err := store.CreateGuest(ctx, "Selection test", "Asia/Tokyo", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID) }()
	if provider, err := store.GetCalendarProvider(ctx, user.ID); err != nil || provider != "none" {
		t.Fatalf("default=%s err=%v", provider, err)
	}
	if err := store.SetCalendarProvider(ctx, user.ID, "google"); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	operation := make(chan error, 1)
	go func() {
		operation <- store.WithCalendarLock(ctx, user.ID, func() error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			provider, err := store.GetCalendarProvider(ctx, user.ID)
			if err == nil && provider != "google" {
				t.Errorf("selection changed during external operation: %s", provider)
			}
			return err
		})
	}()
	<-entered
	switched := make(chan error, 1)
	go func() { switched <- store.SetCalendarProvider(ctx, user.ID, "icloud") }()
	select {
	case err := <-switched:
		t.Fatalf("selection switched before provider completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-operation; err != nil {
		t.Fatal(err)
	}
	if err := <-switched; err != nil {
		t.Fatal(err)
	}
	if provider, err := store.GetCalendarProvider(ctx, user.ID); err != nil || provider != "icloud" {
		t.Fatalf("selection=%s err=%v", provider, err)
	}
	if err := store.SetCalendarProvider(ctx, user.ID, "google,icloud"); err == nil {
		t.Fatal("database accepted multiple providers")
	}
}
