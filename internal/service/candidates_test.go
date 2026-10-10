package service

import (
	"context"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
)

func TestCalculateCandidates(t *testing.T) {
	base := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	event := domain.Event{WindowStartAt: base, WindowEndAt: base.Add(4 * time.Hour), DurationMinutes: 60}
	availability := []domain.ParticipantAvailability{
		{
			Participant: domain.Participant{ID: "participant-1"},
			Intervals:   []domain.AvailabilityInterval{{StartAt: base, EndAt: base.Add(3 * time.Hour)}},
		},
		{
			Participant: domain.Participant{ID: "participant-2"},
			Intervals: []domain.AvailabilityInterval{
				{StartAt: base.Add(time.Hour), EndAt: base.Add(2 * time.Hour)},
				{StartAt: base.Add(3 * time.Hour), EndAt: base.Add(4 * time.Hour)},
			},
		},
	}

	got := calculateCandidates(event, availability, 20)
	if len(got) != 4 {
		t.Fatalf("candidate count = %d, want 4", len(got))
	}
	if !got[0].StartAt.Equal(base.Add(time.Hour)) || got[0].AvailableParticipantCount != 2 {
		t.Errorf("first candidate = %#v, want 01:00 with two participants", got[0])
	}
}

func TestCandidatesAllowsWholeViewForGroupScheduling(t *testing.T) {
	base := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{
				SchedulingMode: "groups", WindowStartAt: base,
				WindowEndAt: base.Add(2 * time.Hour), DurationMinutes: 60,
				ResultsVisibleToParticipants: true,
			}, nil
		},
		listAvailability: func(context.Context, string, string) ([]domain.ParticipantAvailability, error) {
			return []domain.ParticipantAvailability{{
				Participant: domain.Participant{ID: "participant-1"},
				Intervals: []domain.AvailabilityInterval{{
					StartAt: base, EndAt: base.Add(2 * time.Hour),
				}},
			}}, nil
		},
	}

	candidates, err := New(store, time.Hour, "").Candidates(
		context.Background(), "event", "user", 20,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 || candidates[0].TotalParticipantCount != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestCalculateCandidatesRespectsLimit(t *testing.T) {
	base := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	event := domain.Event{WindowStartAt: base, WindowEndAt: base.Add(3 * time.Hour), DurationMinutes: 60}
	availability := []domain.ParticipantAvailability{{
		Participant: domain.Participant{ID: "participant-1"},
		Intervals: []domain.AvailabilityInterval{
			{StartAt: base, EndAt: base.Add(3 * time.Hour)},
		},
	}}

	if got := calculateCandidates(event, availability, 1); len(got) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(got))
	}
}
