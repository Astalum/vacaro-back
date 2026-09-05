package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := New(pool)

	owner, err := store.CreateGuest(ctx, "Repository Owner", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	member, err := store.CreateGuest(ctx, "Repository Member", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var eventID string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if eventID != "" {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM events WHERE id = $1", eventID); err != nil {
				t.Errorf("cleanup event: %v", err)
				return
			}
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1 OR id = $2", owner.ID, member.ID); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})

	updatedOwner, err := store.UpdateUserName(ctx, owner.ID, "Updated Owner")
	if err != nil || updatedOwner.DisplayName != "Updated Owner" {
		t.Fatalf("UpdateUserName() = %#v, %v", updatedOwner, err)
	}
	if _, err := store.GetUser(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}

	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	initialTokenHash := randomHash(t)
	rotatedTokenHash := randomHash(t)
	event, err := store.CreateEvent(ctx, CreateEventParams{
		CreatedByUserID: owner.ID, InviteTokenHash: initialTokenHash, Title: "Repository Event",
		TimeZone: "Asia/Tokyo", WindowStartAt: start, WindowEndAt: start.Add(4 * time.Hour),
		DurationMinutes: 60, OrganizerName: updatedOwner.DisplayName,
	})
	if err != nil {
		t.Fatal(err)
	}
	eventID = event.ID

	if _, err := store.GetEventForUser(ctx, event.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if events, err := store.ListEvents(ctx, owner.ID, "open", nil, nil, 10); err != nil || len(events) != 1 {
		t.Fatalf("ListEvents() = %#v, %v", events, err)
	}
	if _, err := store.GetEventByInviteHash(ctx, initialTokenHash); err != nil {
		t.Fatal(err)
	}

	description := "updated"
	event, err = store.UpdateEvent(ctx, UpdateEventParams{
		EventID: event.ID, UserID: owner.ID, Title: "Updated Event", Description: &description,
		TimeZone: "Asia/Tokyo", WindowStartAt: start, WindowEndAt: start.Add(4 * time.Hour), DurationMinutes: 60,
	})
	if err != nil || event.Title != "Updated Event" {
		t.Fatalf("UpdateEvent() = %#v, %v", event, err)
	}
	if err := store.RotateInviteToken(ctx, event.ID, owner.ID, rotatedTokenHash); err != nil {
		t.Fatal(err)
	}

	participant, err := store.JoinEvent(ctx, rotatedTokenHash, member.ID, "Member")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.JoinEvent(ctx, rotatedTokenHash, member.ID, "Member"); !errors.Is(err, domain.ErrAlreadyJoined) {
		t.Fatalf("duplicate JoinEvent() error = %v", err)
	}
	if participants, err := store.ListParticipants(ctx, event.ID, owner.ID); err != nil || len(participants) != 2 {
		t.Fatalf("ListParticipants() = %#v, %v", participants, err)
	}

	interval := domain.AvailabilityInterval{StartAt: start, EndAt: start.Add(2 * time.Hour), Source: "manual"}
	created, err := store.ReplaceAvailability(ctx, event.ID, member.ID, []domain.AvailabilityInterval{interval})
	if err != nil || len(created) != 1 {
		t.Fatalf("ReplaceAvailability() = %#v, %v", created, err)
	}
	overlapping := []domain.AvailabilityInterval{
		interval,
		{StartAt: start.Add(time.Hour), EndAt: start.Add(3 * time.Hour), Source: "manual"},
	}
	if _, err := store.ReplaceAvailability(ctx, event.ID, member.ID, overlapping); err == nil {
		t.Fatal("overlapping ReplaceAvailability() error = nil")
	}
	intervals, status, err := store.GetOwnAvailability(ctx, event.ID, member.ID)
	if err != nil || status != "submitted" || len(intervals) != 1 {
		t.Fatalf("GetOwnAvailability() = %#v, %q, %v", intervals, status, err)
	}
	if availability, err := store.ListAvailability(ctx, event.ID, owner.ID); err != nil || len(availability) != 2 {
		t.Fatalf("ListAvailability() = %#v, %v", availability, err)
	}

	confirmed, err := store.ConfirmEvent(ctx, event.ID, owner.ID, start, start.Add(time.Hour))
	if err != nil || confirmed.Status != "confirmed" {
		t.Fatalf("ConfirmEvent() = %#v, %v", confirmed, err)
	}
	reopened, err := store.ReopenEvent(ctx, event.ID, owner.ID)
	if err != nil || reopened.Status != "open" {
		t.Fatalf("ReopenEvent() = %#v, %v", reopened, err)
	}
	if err := store.LeaveEvent(ctx, event.ID, owner.ID); !errors.Is(err, domain.ErrOrganizerCannotLeave) {
		t.Fatalf("organizer LeaveEvent() error = %v", err)
	}
	if err := store.LeaveEvent(ctx, event.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if participant.ID == "" {
		t.Fatal("participant ID is empty")
	}
	if err := store.CancelEvent(ctx, event.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := New(pool)

	registered, err := store.UpsertGoogleUser(ctx, "", "subject-"+randomHash(t), "registered@example.com", "Registered")
	if err != nil {
		t.Fatal(err)
	}
	googleEventID := ""
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if googleEventID != "" {
			if _, err := pool.Exec(cleanupCtx, "DELETE FROM events WHERE id = $1", googleEventID); err != nil {
				t.Errorf("cleanup Google event: %v", err)
				return
			}
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", registered.ID); err != nil {
			t.Errorf("cleanup registered user: %v", err)
		}
	})
	guest, err := store.CreateGuest(ctx, "Guest", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	event, err := store.CreateEvent(ctx, CreateEventParams{
		CreatedByUserID: guest.ID, InviteTokenHash: randomHash(t), Title: "Merge Event",
		TimeZone: "Asia/Tokyo", WindowStartAt: start, WindowEndAt: start.Add(2 * time.Hour),
		DurationMinutes: 30, OrganizerName: guest.DisplayName,
	})
	if err != nil {
		t.Fatal(err)
	}
	googleEventID = event.ID

	var subject string
	if err := pool.QueryRow(ctx, "SELECT provider_subject FROM auth_accounts WHERE user_id = $1", registered.ID).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	merged, err := store.UpsertGoogleUser(ctx, guest.ID, subject, "updated@example.com", "Updated Registered")
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != registered.ID || merged.AccountType != "registered" || merged.Email == nil || *merged.Email != "updated@example.com" {
		t.Fatalf("merged user = %#v", merged)
	}
	if hasGoogleAccount, err := store.HasGoogleAccount(ctx, registered.ID); err != nil || !hasGoogleAccount {
		t.Fatalf("HasGoogleAccount() = %v, %v", hasGoogleAccount, err)
	}
	if _, err := store.GetUser(ctx, guest.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("guest still exists: %v", err)
	}
	gotEvent, err := store.GetEventForUser(ctx, event.ID, registered.ID)
	if err != nil || gotEvent.CreatedByUserID != registered.ID {
		t.Fatalf("merged event = %#v, %v", gotEvent, err)
	}

	ciphertext := []byte("encrypted-refresh-token")
	scopes := []string{"https://www.googleapis.com/auth/calendar.freebusy"}
	if err := store.SaveGoogleCalendarConnection(ctx, registered.ID, ciphertext, scopes); err != nil {
		t.Fatal(err)
	}
	gotCiphertext, gotScopes, err := store.GetGoogleCalendarConnection(ctx, registered.ID)
	if err != nil || string(gotCiphertext) != string(ciphertext) || len(gotScopes) != 1 || gotScopes[0] != scopes[0] {
		t.Fatalf("GetGoogleCalendarConnection() = %q, %#v, %v", gotCiphertext, gotScopes, err)
	}
	deleted, err := store.DeleteGoogleCalendarConnection(ctx, registered.ID)
	if err != nil || string(deleted) != string(ciphertext) {
		t.Fatalf("DeleteGoogleCalendarConnection() = %q, %v", deleted, err)
	}
	if _, _, err := store.GetGoogleCalendarConnection(ctx, registered.ID); !errors.Is(err, domain.ErrCalendarNotConnected) {
		t.Fatalf("GetGoogleCalendarConnection() after delete error = %v", err)
	}
}

func randomHash(t *testing.T) string {
	t.Helper()
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value[:])
}
