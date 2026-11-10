package service

import (
	"context"
	"errors"
	"github.com/astalum/vacaro-back/internal/domain"
	"testing"
	"time"
)

func TestSetAvailabilityDefault(t *testing.T) {
	for _, value := range []string{"available", "unavailable"} {
		t.Run(value, func(t *testing.T) {
			store := storeStub{setAvailabilityDefault: func(_ context.Context, id, got string) (domain.User, error) {
				if id != "user-1" || got != value {
					t.Fatalf("unexpected update: %q %q", id, got)
				}
				return domain.User{ID: id, AvailabilityDefault: got}, nil
			}}
			application := New(store, time.Hour, "")
			user, err := application.SetAvailabilityDefault(context.Background(), "user-1", value)
			if err != nil || user.AvailabilityDefault != value {
				t.Fatalf("got %#v, %v", user, err)
			}
		})
	}
}

func TestSetAvailabilityDefaultRejectsInvalidValues(t *testing.T) {
	application := New(storeStub{}, time.Hour, "")
	for _, value := range []string{"", "busy", "AVAILABLE"} {
		_, err := application.SetAvailabilityDefault(context.Background(), "user-1", value)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["availability_default"] == "" {
			t.Fatalf("value %q: %v", value, err)
		}
	}
}

func TestSetAvailabilityDefaultRejectsMissingUser(t *testing.T) {
	application := New(storeStub{getUser: func(context.Context, string) (domain.User, error) {
		return domain.User{}, domain.ErrNotFound
	}}, time.Hour, "")
	if _, err := application.SetAvailabilityDefault(context.Background(), "user-1", "available"); err == nil {
		t.Fatal("missing user updated preferences")
	}
}
