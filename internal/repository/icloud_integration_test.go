package repository

import (
	"context"
	"errors"
	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestICloudStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	user, err := store.CreateGuest(ctx, "iCloud User", "Asia/Tokyo", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID) }()
	cipher := []byte("encrypted credentials")
	if err := store.SaveICloudConnection(ctx, user.ID, cipher, "vacaro-calendar"); err != nil {
		t.Fatal(err)
	}
	got, id, err := store.GetICloudConnection(ctx, user.ID)
	if err != nil || string(got) != string(cipher) || id != "vacaro-calendar" {
		t.Fatalf("connection: %q,%q,%v", got, id, err)
	}
	start := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	event, err := store.CreateEvent(ctx, CreateEventParams{CreatedByUserID: user.ID, InviteTokenHash: randomHash(t), Title: "iCloud test", WindowStartAt: start, WindowEndAt: start.Add(time.Hour), DurationMinutes: 30, OrganizerName: "Owner", SchedulingMode: "groups"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM events WHERE id=$1`, event.ID) }()
	participants, err := store.ListParticipants(ctx, event.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	group, err := store.CreateGroup(ctx, CreateGroupParams{EventID: event.ID, UserID: user.ID, Name: "Group", DurationMinutes: 30, ParticipantIDs: []string{participants[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	for _, groupID := range []*string{nil, &group.ID} {
		if _, err := store.UpsertICloudCalendarSync(ctx, event.ID, groupID, user.ID, "vacaro-calendar", "failed"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpsertICloudCalendarSync(ctx, event.ID, groupID, user.ID, "vacaro-calendar", "synced"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.ListICloudCalendarSyncs(ctx, event.ID, user.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("syncs: %v,%v", rows, err)
	}
	if err := store.SaveICloudConnection(ctx, user.ID, cipher, "other-calendar"); err != nil {
		t.Fatal(err)
	}
	rows, err = store.ListICloudCalendarSyncs(ctx, event.ID, user.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("stale syncs shown: %v,%v", rows, err)
	}
	if err := store.ClearAllICloudCalendarSyncs(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteICloudConnection(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetICloudConnection(ctx, user.ID); !errors.Is(err, domain.ErrICloudNotConnected) {
		t.Fatalf("disconnect: %v", err)
	}
}
func TestGuestICloudConnectionSurvivesGoogleMerge(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool)
	subject := "icloud-merge-" + randomHash(t)
	target, err := store.UpsertGoogleUser(ctx, "", subject, "registered@example.com", "User")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, target.ID) }()
	guest, err := store.CreateGuest(ctx, "Guest", "Asia/Tokyo", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, guest.ID) }()
	if err := store.SaveICloudConnection(ctx, guest.ID, []byte("sealed guest credentials"), "guest-vacaro-calendar"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertGoogleUser(ctx, guest.ID, subject, "registered@example.com", "User"); err != nil {
		t.Fatal(err)
	}
	cipher, id, err := store.GetICloudConnection(ctx, target.ID)
	if err != nil || string(cipher) != "sealed guest credentials" || id != "guest-vacaro-calendar" {
		t.Fatalf("merge lost connection: %q %q %v", cipher, id, err)
	}
}
