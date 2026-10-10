package service

import (
	"context"
	"testing"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/astalum/vacaro-back/internal/repository"
)

func TestCreateGroupUsesEventDurationAndTrimsName(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	var got repository.CreateGroupParams
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{
				CreatedByUserID: "owner", Status: "open", SchedulingMode: "groups",
				WindowStartAt: base, WindowEndAt: base.Add(4 * time.Hour), DurationMinutes: 60,
			}, nil
		},
		listParticipants: func(context.Context, string, string) ([]domain.Participant, error) {
			return []domain.Participant{{ID: "participant-1"}}, nil
		},
		createGroup: func(_ context.Context, params repository.CreateGroupParams) (domain.SchedulingGroup, error) {
			got = params
			return domain.SchedulingGroup{Name: params.Name, DurationMinutes: params.DurationMinutes}, nil
		},
	}
	service := New(store, time.Hour, "")
	if _, err := service.CreateGroup(context.Background(), "event", "owner", CreateGroupInput{
		Name: "  Design  ", ParticipantIDs: []string{"participant-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Design" || got.DurationMinutes != 60 || len(got.ParticipantIDs) != 1 {
		t.Fatalf("CreateGroup params = %#v", got)
	}
}

func TestCreateGroupRejectsDuplicateMembers(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{
				CreatedByUserID: "owner", Status: "open", SchedulingMode: "groups",
				WindowStartAt: base, WindowEndAt: base.Add(4 * time.Hour), DurationMinutes: 60,
			}, nil
		},
		listParticipants: func(context.Context, string, string) ([]domain.Participant, error) {
			return []domain.Participant{{ID: "participant-1"}}, nil
		},
	}
	service := New(store, time.Hour, "")
	if _, err := service.CreateGroup(context.Background(), "event", "owner", CreateGroupInput{
		Name: "Design", ParticipantIDs: []string{"participant-1", "participant-1"},
	}); err == nil {
		t.Fatal("CreateGroup() error = nil, want duplicate member validation")
	}
}

func TestGroupCandidatesUsesGroupDurationAndMembers(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{
				SchedulingMode: "groups", WindowStartAt: base,
				WindowEndAt: base.Add(3 * time.Hour), DurationMinutes: 120,
			}, nil
		},
		getGroup: func(context.Context, string, string, string) (domain.SchedulingGroup, error) {
			return domain.SchedulingGroup{DurationMinutes: 30}, nil
		},
		listGroupAvailability: func(context.Context, string, string, string) ([]domain.ParticipantAvailability, error) {
			return []domain.ParticipantAvailability{
				{
					Participant: domain.Participant{ID: "member-1"},
					Intervals:   []domain.AvailabilityInterval{{StartAt: base, EndAt: base.Add(time.Hour)}},
				},
			}, nil
		},
	}
	service := New(store, time.Hour, "")
	candidates, err := service.GroupCandidates(context.Background(), "event", "group", "member", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 || candidates[0].EndAt.Sub(candidates[0].StartAt) != 30*time.Minute {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].TotalParticipantCount != 1 {
		t.Fatalf("total participant count = %d", candidates[0].TotalParticipantCount)
	}
}

func TestConfirmGroupRequiresMember(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{
				CreatedByUserID: "owner", Status: "open", SchedulingMode: "groups",
				WindowStartAt: base, WindowEndAt: base.Add(3 * time.Hour),
			}, nil
		},
		getGroup: func(context.Context, string, string, string) (domain.SchedulingGroup, error) {
			return domain.SchedulingGroup{DurationMinutes: 30, Members: []domain.Participant{}}, nil
		},
	}
	service := New(store, time.Hour, "")
	if _, err := service.ConfirmGroup(context.Background(), "event", "group", "owner", base, base.Add(30*time.Minute)); err == nil {
		t.Fatal("ConfirmGroup() error = nil, want member validation")
	}
}

func TestConfirmGroupAllowsConfirmedEvent(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	called := false
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return domain.Event{
				CreatedByUserID: "owner", Status: "confirmed", SchedulingMode: "groups",
				WindowStartAt: base, WindowEndAt: base.Add(3 * time.Hour),
			}, nil
		},
		getGroup: func(context.Context, string, string, string) (domain.SchedulingGroup, error) {
			return domain.SchedulingGroup{
				DurationMinutes: 30,
				Members:         []domain.Participant{{ID: "participant"}},
			}, nil
		},
		confirmGroup: func(context.Context, string, string, string, time.Time, time.Time) (domain.SchedulingGroup, error) {
			called = true
			return domain.SchedulingGroup{}, nil
		},
	}
	service := New(store, time.Hour, "")
	if _, err := service.ConfirmGroup(context.Background(), "event", "group", "owner", base, base.Add(30*time.Minute)); err != nil {
		t.Fatalf("ConfirmGroup() error = %v", err)
	}
	if !called {
		t.Fatal("ConfirmGroup() did not call store")
	}
}

func TestConfirmGroupSyncsOnlyGroupMemberCalendar(t *testing.T) {
	base := time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC)
	event := domain.Event{
		ID: "event-1", CreatedByUserID: "owner", Title: "面談", TimeZone: "Asia/Tokyo",
		Status: "open", SchedulingMode: "groups", WindowStartAt: base,
		WindowEndAt: base.Add(3 * time.Hour),
	}
	group := domain.SchedulingGroup{
		ID: "group-1", Name: "営業", DurationMinutes: 30,
		Members: []domain.Participant{{EventID: event.ID, UserID: "member-user"}},
	}
	store := storeStub{
		getEventForUser: func(context.Context, string, string) (domain.Event, error) {
			return event, nil
		},
		getGroup: func(context.Context, string, string, string) (domain.SchedulingGroup, error) {
			return group, nil
		},
		confirmGroup: func(context.Context, string, string, string, time.Time, time.Time) (domain.SchedulingGroup, error) {
			return group, nil
		},
		getGoogleCalendar: func(_ context.Context, userID string) ([]byte, []string, error) {
			if userID != "member-user" {
				t.Fatalf("calendar lookup user = %q", userID)
			}
			return []byte("sealed:refresh-token"), []string{googleclient.CalendarEventsScope}, nil
		},
	}
	var gotEvent googleclient.CalendarEvent
	provider := googleProviderStub{upsertEvent: func(_ context.Context, _ string, entry googleclient.CalendarEvent) error {
		gotEvent = entry
		return nil
	}}
	application := New(store, time.Hour, "https://vacaro.example")
	application.ConfigureGoogle(provider, tokenBoxStub{}, time.Hour)

	if _, err := application.ConfirmGroup(context.Background(), event.ID, group.ID, "owner", base, base.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if gotEvent.Summary != "面談（営業）" || gotEvent.ID != confirmationCalendarEventID(event.ID, group.ID) {
		t.Fatalf("calendar event = %#v", gotEvent)
	}
}
