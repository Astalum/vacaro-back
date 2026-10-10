package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
)

func (s *Service) GoogleEnabled() bool {
	return s.google != nil && s.tokenBox != nil
}

func (s *Service) GoogleLoginURL(state, nonce string) (string, error) {
	if !s.GoogleEnabled() {
		return "", domain.ErrGoogleNotConfigured
	}
	return s.google.LoginAuthURL(state, nonce), nil
}

func (s *Service) CompleteGoogleLogin(ctx context.Context, code, nonce, currentUserID string) (domain.User, time.Time, error) {
	if !s.GoogleEnabled() {
		return domain.User{}, time.Time{}, domain.ErrGoogleNotConfigured
	}
	identity, err := s.google.ExchangeIdentity(ctx, code, nonce)
	if err != nil {
		return domain.User{}, time.Time{}, fmt.Errorf("%w: %v", domain.ErrOAuthFailed, err)
	}
	user, err := s.store.UpsertGoogleUser(ctx, currentUserID, identity.Subject, identity.Email, strings.TrimSpace(identity.Name))
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	return user, time.Now().UTC().Add(s.registeredTTL), nil
}

func (s *Service) GoogleCalendarURL(state string) (string, error) {
	if !s.GoogleEnabled() {
		return "", domain.ErrGoogleNotConfigured
	}
	return s.google.CalendarAuthURL(state), nil
}

func (s *Service) CompleteGoogleCalendar(ctx context.Context, userID, code string) error {
	if !s.GoogleEnabled() {
		return domain.ErrGoogleNotConfigured
	}
	refreshToken, scopes, err := s.google.ExchangeCalendar(ctx, code)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrOAuthFailed, err)
	}
	ciphertext, err := s.tokenBox.Seal([]byte(refreshToken))
	if err != nil {
		return err
	}
	return s.store.SaveGoogleCalendarConnection(ctx, userID, ciphertext, scopes)
}

func (s *Service) DisconnectGoogleCalendar(ctx context.Context, userID string) error {
	if !s.GoogleEnabled() {
		return domain.ErrGoogleNotConfigured
	}
	ciphertext, _, err := s.store.GetGoogleCalendarConnection(ctx, userID)
	if err != nil {
		return err
	}
	plain, err := s.tokenBox.Open(ciphertext)
	if err != nil {
		return err
	}
	if err := s.google.Revoke(ctx, string(plain)); err != nil {
		return fmt.Errorf("revoke Google Calendar token: %w", err)
	}
	_, err = s.store.DeleteGoogleCalendarConnection(ctx, userID)
	return err
}

type GoogleCalendarOptions struct {
	Calendars    []googleclient.Calendar
	WriteEnabled bool
}

func (s *Service) ListGoogleCalendars(ctx context.Context, userID string) (GoogleCalendarOptions, error) {
	if !s.GoogleEnabled() {
		return GoogleCalendarOptions{}, domain.ErrGoogleNotConfigured
	}
	ciphertext, scopes, err := s.store.GetGoogleCalendarConnection(ctx, userID)
	if err != nil {
		return GoogleCalendarOptions{}, err
	}
	plain, err := s.tokenBox.Open(ciphertext)
	if err != nil {
		return GoogleCalendarOptions{}, err
	}
	calendars, err := s.google.ListCalendars(ctx, string(plain))
	if err != nil {
		return GoogleCalendarOptions{}, fmt.Errorf("%w: %v", domain.ErrOAuthFailed, err)
	}
	return GoogleCalendarOptions{
		Calendars:    calendars,
		WriteEnabled: hasGoogleCalendarScope(scopes, googleclient.CalendarEventsScope),
	}, nil
}

func (s *Service) PreviewGoogleCalendarAvailability(ctx context.Context, eventID, userID string, calendarIDs []string) ([]domain.AvailabilityInterval, error) {
	if !s.GoogleEnabled() {
		return nil, domain.ErrGoogleNotConfigured
	}
	if len(calendarIDs) == 0 {
		calendarIDs = []string{"primary"}
	}
	if len(calendarIDs) > 50 {
		return nil, &ValidationError{Fields: map[string]string{"calendar_ids": "50件以下で指定してください"}}
	}
	for _, id := range calendarIDs {
		if strings.TrimSpace(id) == "" {
			return nil, &ValidationError{Fields: map[string]string{"calendar_ids": "空のIDは指定できません"}}
		}
	}
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return nil, err
	}
	ciphertext, _, err := s.store.GetGoogleCalendarConnection(ctx, userID)
	if err != nil {
		return nil, err
	}
	plain, err := s.tokenBox.Open(ciphertext)
	if err != nil {
		return nil, err
	}
	busy, err := s.google.FreeBusy(ctx, string(plain), calendarIDs, event.WindowStartAt, event.WindowEndAt, event.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrOAuthFailed, err)
	}
	return subtractBusy(event.WindowStartAt, event.WindowEndAt, busy), nil
}

func subtractBusy(startAt, endAt time.Time, busy []googleclient.BusyInterval) []domain.AvailabilityInterval {
	clipped := make([]googleclient.BusyInterval, 0, len(busy))
	for _, interval := range busy {
		start, end := interval.StartAt.UTC(), interval.EndAt.UTC()
		if start.Before(startAt) {
			start = startAt
		}
		if end.After(endAt) {
			end = endAt
		}
		if start.Before(end) {
			clipped = append(clipped, googleclient.BusyInterval{StartAt: start, EndAt: end})
		}
	}
	sort.Slice(clipped, func(i, j int) bool { return clipped[i].StartAt.Before(clipped[j].StartAt) })
	merged := make([]googleclient.BusyInterval, 0, len(clipped))
	for _, interval := range clipped {
		if len(merged) == 0 || interval.StartAt.After(merged[len(merged)-1].EndAt) {
			merged = append(merged, interval)
			continue
		}
		if interval.EndAt.After(merged[len(merged)-1].EndAt) {
			merged[len(merged)-1].EndAt = interval.EndAt
		}
	}
	free := make([]domain.AvailabilityInterval, 0, len(merged)+1)
	cursor := startAt
	for _, interval := range merged {
		if cursor.Before(interval.StartAt) {
			free = append(free, domain.AvailabilityInterval{StartAt: cursor, EndAt: interval.StartAt, Source: "google_calendar"})
		}
		if interval.EndAt.After(cursor) {
			cursor = interval.EndAt
		}
	}
	if cursor.Before(endAt) {
		free = append(free, domain.AvailabilityInterval{StartAt: cursor, EndAt: endAt, Source: "google_calendar"})
	}
	return free
}
