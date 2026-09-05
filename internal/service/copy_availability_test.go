package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
)

func TestCopyOwnAvailabilityMergesWithCurrentAnswer(t *testing.T) {
	windowStart := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(6 * time.Hour)
	var stored []domain.AvailabilityInterval
	store := storeStub{
		hasGoogleAccount: func(context.Context, string) (bool, error) { return true, nil },
		getOwnAvailability: func(_ context.Context, eventID, _ string) ([]domain.AvailabilityInterval, string, error) {
			if eventID == "source" {
				return []domain.AvailabilityInterval{
					{StartAt: windowStart.Add(-time.Hour), EndAt: windowStart.Add(30 * time.Minute)},
					{StartAt: windowStart.Add(2 * time.Hour), EndAt: windowStart.Add(4 * time.Hour)},
					{StartAt: windowStart.Add(5 * time.Hour), EndAt: windowEnd.Add(time.Hour)},
				}, "submitted", nil
			}
			return []domain.AvailabilityInterval{{StartAt: windowStart, EndAt: windowStart.Add(time.Hour)}}, "submitted", nil
		},
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{Status: "open", WindowStartAt: windowStart, WindowEndAt: windowEnd}, nil
		},
		replaceAvailability: func(_ context.Context, _, _ string, intervals []domain.AvailabilityInterval) ([]domain.AvailabilityInterval, error) {
			stored = intervals
			return intervals, nil
		},
	}
	application := New(store, time.Hour, "http://localhost")
	got, err := application.CopyOwnAvailability(context.Background(), "target", "source", "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || len(stored) != 3 {
		t.Fatalf("intervals = %#v", got)
	}
	want := [][2]time.Time{
		{windowStart, windowStart.Add(time.Hour)},
		{windowStart.Add(2 * time.Hour), windowStart.Add(4 * time.Hour)},
		{windowStart.Add(5 * time.Hour), windowEnd},
	}
	for i, interval := range got {
		if !interval.StartAt.Equal(want[i][0]) || !interval.EndAt.Equal(want[i][1]) || interval.Source != "manual" {
			t.Errorf("interval[%d] = %#v, want %s-%s", i, interval, want[i][0], want[i][1])
		}
	}
}

func TestCopyOwnAvailabilityRequiresGoogleAccount(t *testing.T) {
	application := New(storeStub{hasGoogleAccount: func(context.Context, string) (bool, error) { return false, nil }}, time.Hour, "http://localhost")
	_, err := application.CopyOwnAvailability(context.Background(), "target", "source", "guest")
	if !errors.Is(err, domain.ErrGoogleAuthRequired) {
		t.Fatalf("CopyOwnAvailability() error = %v", err)
	}
}

func TestCopyOwnAvailabilityRejectsNoOverlappingAnswer(t *testing.T) {
	targetStart := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	store := storeStub{
		hasGoogleAccount: func(context.Context, string) (bool, error) { return true, nil },
		getOwnAvailability: func(_ context.Context, eventID, _ string) ([]domain.AvailabilityInterval, string, error) {
			if eventID == "source" {
				return []domain.AvailabilityInterval{{StartAt: targetStart.Add(-4 * time.Hour), EndAt: targetStart.Add(-3 * time.Hour)}}, "submitted", nil
			}
			return nil, "pending", nil
		},
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{Status: "open", WindowStartAt: targetStart, WindowEndAt: targetStart.Add(2 * time.Hour)}, nil
		},
	}
	application := New(store, time.Hour, "http://localhost")
	_, err := application.CopyOwnAvailability(context.Background(), "target", "source", "user")
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("CopyOwnAvailability() error = %v", err)
	}
}
