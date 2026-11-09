package service

import (
	"context"
	"errors"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
)

type CalendarPreference struct {
	Provider string `json:"provider"`
}

func (s *Service) GetCalendarPreference(ctx context.Context, id string) (CalendarPreference, error) {
	if _, err := s.GetUser(ctx, id); err != nil {
		return CalendarPreference{}, err
	}
	provider, err := s.store.GetCalendarProvider(ctx, id)
	return CalendarPreference{provider}, err
}
func (s *Service) SetCalendarPreference(ctx context.Context, id, provider string) (CalendarPreference, error) {
	switch provider {
	case "none", "google", "icloud", "timetree":
	default:
		return CalendarPreference{}, &ValidationError{Fields: map[string]string{"provider": "使用するカレンダーを1つ選択してください"}}
	}
	if _, err := s.GetUser(ctx, id); err != nil {
		return CalendarPreference{}, err
	}
	if err := s.store.SetCalendarProvider(ctx, id, provider); err != nil {
		return CalendarPreference{}, err
	}
	return CalendarPreference{provider}, nil
}
func calendarTransport(provider string) string {
	if provider == "timetree" {
		return "google"
	}
	return provider
}
func (s *Service) requireCalendarProvider(ctx context.Context, id, transport string, importing bool) error {
	provider, err := s.store.GetCalendarProvider(ctx, id)
	if err != nil {
		return err
	}
	if calendarTransport(provider) != transport || (importing && provider == "timetree") {
		return domain.ErrInactiveCalendar
	}
	return nil
}
func (s *Service) withActiveCalendar(ctx context.Context, id, transport string, importing bool, fn func() error) error {
	return s.store.WithCalendarLock(ctx, id, func() error {
		if _, err := s.GetUser(ctx, id); err != nil {
			return err
		}
		if err := s.requireCalendarProvider(ctx, id, transport, importing); err != nil {
			return err
		}
		return fn()
	})
}
func calendarResult[T any](s *Service, ctx context.Context, id, transport string, importing bool, fn func() (T, error)) (T, error) {
	var result T
	err := s.withActiveCalendar(ctx, id, transport, importing, func() error { var err error; result, err = fn(); return err })
	return result, err
}

// Resolve and use one destination under one lock for each participant. A switch
// during confirmation cannot send the same confirmation through both branches.
func (s *Service) syncSelectedCalendars(ctx context.Context, participants []domain.Participant, event googleclient.CalendarEvent, eventID, groupID string) {
	seen := map[string]bool{}
	for _, p := range participants {
		if seen[p.UserID] {
			continue
		}
		seen[p.UserID] = true
		err := s.store.WithCalendarLock(ctx, p.UserID, func() error {
			provider, err := s.store.GetCalendarProvider(ctx, p.UserID)
			if err != nil {
				return err
			}
			switch calendarTransport(provider) {
			case "google":
				if !s.GoogleEnabled() {
					return nil
				}
				_, err = s.syncConfirmationCalendarForUserUnlocked(ctx, eventID, optionalGroupID(groupID), p.UserID, event)
			case "icloud":
				if !s.ICloudEnabled() {
					return nil
				}
				_, err = s.syncICloudCalendarForUserUnlocked(ctx, eventID, optionalGroupID(groupID), p.UserID, event)
			}
			return err
		})
		if err != nil && !errors.Is(err, domain.ErrCalendarNotConnected) && !errors.Is(err, domain.ErrICloudNotConnected) && !errors.Is(err, domain.ErrCalendarWritePermission) {
			logCalendarSyncFailure(ctx, "sync selected calendar", eventID, groupID, err)
		}
	}
}
