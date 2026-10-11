package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
)

func TestRequireEventAcceptingResponses(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	tests := []struct {
		name      string
		event     domain.Event
		wantError error
	}{
		{name: "no deadline", event: domain.Event{Status: "open"}},
		{name: "before deadline", event: domain.Event{Status: "open", ResponseDeadlineAt: &future}},
		{name: "at deadline", event: domain.Event{Status: "open", ResponseDeadlineAt: &now}, wantError: domain.ErrResponseDeadlinePassed},
		{name: "after deadline", event: domain.Event{Status: "open", ResponseDeadlineAt: &past}, wantError: domain.ErrResponseDeadlinePassed},
		{name: "closed event", event: domain.Event{Status: "confirmed", ResponseDeadlineAt: &past}, wantError: domain.ErrEventNotOpen},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := requireEventAcceptingResponses(test.event, now)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("requireEventAcceptingResponses() error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func TestJoinEventRejectsPassedResponseDeadline(t *testing.T) {
	deadline := time.Now().UTC().Add(-time.Minute)
	joinCalled := false
	store := storeStub{
		getEventByInvite: func(context.Context, string) (domain.Event, error) {
			return domain.Event{Status: "open", ResponseDeadlineAt: &deadline}, nil
		},
		joinEvent: func(context.Context, string, string, string) (domain.Participant, error) {
			joinCalled = true
			return domain.Participant{}, nil
		},
	}
	application := New(store, time.Hour, "")

	_, err := application.JoinEvent(context.Background(), "invite-token", "user", "Member")
	if !errors.Is(err, domain.ErrResponseDeadlinePassed) {
		t.Fatalf("JoinEvent() error = %v", err)
	}
	if joinCalled {
		t.Fatal("JoinEvent() wrote a participant after the response deadline")
	}
}

func TestReplaceAvailabilityRejectsPassedResponseDeadline(t *testing.T) {
	deadline := time.Now().UTC().Add(-time.Minute)
	replaceCalled := false
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{Status: "open", ResponseDeadlineAt: &deadline}, nil
		},
		replaceAvailability: func(context.Context, string, string, []domain.AvailabilityInterval) ([]domain.AvailabilityInterval, error) {
			replaceCalled = true
			return nil, nil
		},
	}
	application := New(store, time.Hour, "")

	_, err := application.ReplaceAvailability(context.Background(), "event", "user", nil)
	if !errors.Is(err, domain.ErrResponseDeadlinePassed) {
		t.Fatalf("ReplaceAvailability() error = %v", err)
	}
	if replaceCalled {
		t.Fatal("ReplaceAvailability() wrote an answer after the response deadline")
	}
}
