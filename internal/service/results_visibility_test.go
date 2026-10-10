package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
)

func TestUpdateResultsVisibilityRequiresOrganizer(t *testing.T) {
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{CreatedByUserID: "owner"}, nil
		},
	}

	_, err := New(store, time.Hour, "").UpdateResultsVisibility(
		context.Background(), "event", "member", false,
	)
	if !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("UpdateResultsVisibility() error = %v, want permission denied", err)
	}
}

func TestUpdateResultsVisibilityPersistsOrganizerChoice(t *testing.T) {
	called := false
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{CreatedByUserID: "owner"}, nil
		},
		setResultsVisibility: func(_ context.Context, eventID, userID string, visible bool) (domain.Event, error) {
			called = true
			if eventID != "event" || userID != "owner" || visible {
				t.Fatalf("SetResultsVisibility(%q, %q, %v)", eventID, userID, visible)
			}
			return domain.Event{ID: eventID, ResultsVisibleToParticipants: visible}, nil
		},
	}

	event, err := New(store, time.Hour, "").UpdateResultsVisibility(
		context.Background(), "event", "owner", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !called || event.ResultsVisibleToParticipants {
		t.Fatalf("event = %#v, called = %v", event, called)
	}
}

func TestResultsAPIsRejectParticipantWhenResultsAreHidden(t *testing.T) {
	listCalled := false
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{CreatedByUserID: "owner", ResultsVisibleToParticipants: false}, nil
		},
		listAvailability: func(context.Context, string, string) ([]domain.ParticipantAvailability, error) {
			listCalled = true
			return nil, nil
		},
	}
	service := New(store, time.Hour, "")

	if _, err := service.ListAvailability(context.Background(), "event", "member"); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("ListAvailability() error = %v, want permission denied", err)
	}
	if _, err := service.Candidates(context.Background(), "event", "member", 20); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("Candidates() error = %v, want permission denied", err)
	}
	if listCalled {
		t.Fatal("hidden availability was loaded")
	}
}

func TestGroupResultsAPIsRejectParticipantWhenResultsAreHidden(t *testing.T) {
	groupDataCalled := false
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{
				CreatedByUserID: "owner", SchedulingMode: "groups",
				ResultsVisibleToParticipants: false,
			}, nil
		},
		getGroup: func(context.Context, string, string, string) (domain.SchedulingGroup, error) {
			groupDataCalled = true
			return domain.SchedulingGroup{}, nil
		},
		listGroupAvailability: func(context.Context, string, string, string) ([]domain.ParticipantAvailability, error) {
			groupDataCalled = true
			return nil, nil
		},
	}
	service := New(store, time.Hour, "")

	if _, err := service.ListGroupAvailability(context.Background(), "event", "group", "member"); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("ListGroupAvailability() error = %v, want permission denied", err)
	}
	if _, err := service.GroupCandidates(context.Background(), "event", "group", "member", 20); !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("GroupCandidates() error = %v, want permission denied", err)
	}
	if groupDataCalled {
		t.Fatal("hidden group results were loaded")
	}
}
