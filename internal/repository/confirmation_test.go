package repository

import (
	"reflect"
	"testing"
	"time"
)

func TestSubtractAvailabilityInterval(t *testing.T) {
	base := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		unavailableStart time.Time
		unavailableEnd   time.Time
		want             []availabilityRemainder
	}{
		{name: "outside", unavailableStart: base.Add(-2 * time.Hour), unavailableEnd: base.Add(-time.Hour), want: []availabilityRemainder{{StartAt: base, EndAt: base.Add(4 * time.Hour)}}},
		{name: "covers interval", unavailableStart: base.Add(-time.Hour), unavailableEnd: base.Add(5 * time.Hour), want: nil},
		{name: "trims start", unavailableStart: base.Add(-time.Hour), unavailableEnd: base.Add(time.Hour), want: []availabilityRemainder{{StartAt: base.Add(time.Hour), EndAt: base.Add(4 * time.Hour)}}},
		{name: "trims end", unavailableStart: base.Add(3 * time.Hour), unavailableEnd: base.Add(5 * time.Hour), want: []availabilityRemainder{{StartAt: base, EndAt: base.Add(3 * time.Hour)}}},
		{name: "splits interval", unavailableStart: base.Add(time.Hour), unavailableEnd: base.Add(2 * time.Hour), want: []availabilityRemainder{{StartAt: base, EndAt: base.Add(time.Hour)}, {StartAt: base.Add(2 * time.Hour), EndAt: base.Add(4 * time.Hour)}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := subtractAvailabilityInterval(base, base.Add(4*time.Hour), test.unavailableStart, test.unavailableEnd)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("subtractAvailabilityInterval() = %#v, want %#v", got, test.want)
			}
		})
	}
}
