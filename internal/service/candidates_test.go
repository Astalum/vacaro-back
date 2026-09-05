package service

import (
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
