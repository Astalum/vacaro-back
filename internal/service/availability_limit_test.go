package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
)

func TestReplaceAvailabilityAcceptsFragmentedCalendarImport(t *testing.T) {
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Minute)
	const count = 132
	inputs := make([]IntervalInput, 0, count)
	// Two free windows per day over 66 days can legitimately exceed 100.
	for day := 0; day < 66; day++ {
		for _, offset := range []time.Duration{time.Hour, 5 * time.Hour} {
			from := start.Add(time.Duration(day)*24*time.Hour + offset)
			inputs = append(inputs, IntervalInput{StartAt: from, EndAt: from.Add(time.Hour)})
		}
	}
	var stored []domain.AvailabilityInterval
	application := New(storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{Status: "open", WindowStartAt: start, WindowEndAt: start.Add(66 * 24 * time.Hour)}, nil
		},
		replaceAvailability: func(_ context.Context, _, _ string, intervals []domain.AvailabilityInterval) ([]domain.AvailabilityInterval, error) {
			stored = intervals
			return intervals, nil
		},
	}, time.Hour, "")
	result, err := application.ReplaceAvailability(context.Background(), "event", "user", inputs)
	if err != nil {
		t.Fatalf("valid calendar import was rejected: %v", err)
	}
	if len(result) != count || len(stored) != count {
		t.Fatalf("saved %d / %d intervals, want %d", len(result), len(stored), count)
	}
	for i, interval := range stored {
		if !interval.StartAt.Equal(inputs[i].StartAt) || !interval.EndAt.Equal(inputs[i].EndAt) {
			t.Fatalf("interval %d changed: %#v", i, interval)
		}
	}
}

func TestReplaceAvailabilityStillBoundsImportSize(t *testing.T) {
	application := New(storeStub{}, time.Hour, "")
	_, err := application.ReplaceAvailability(context.Background(), "event", "user", make([]IntervalInput, maxAvailabilityIntervals+1))
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["intervals"] == "" {
		t.Fatalf("oversized input error = %v, want interval validation", err)
	}
}
