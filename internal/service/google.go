package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
)

func (s *Service) GoogleEnabled() bool {
	return s.google != nil && s.tokenBox != nil
}

func (s *Service) GoogleLoginURL(state, nonce, verifier string) (string, error) {
	if !s.GoogleEnabled() {
		return "", domain.ErrGoogleNotConfigured
	}
	return s.google.LoginAuthURL(state, nonce, verifier), nil
}

func (s *Service) CompleteGoogleLogin(ctx context.Context, code, nonce, verifier, currentUserID string) (domain.User, time.Time, error) {
	if !s.GoogleEnabled() {
		return domain.User{}, time.Time{}, domain.ErrGoogleNotConfigured
	}
	identity, err := s.google.ExchangeIdentity(ctx, code, nonce, verifier)
	if err != nil {
		return domain.User{}, time.Time{}, fmt.Errorf("%w: %v", domain.ErrOAuthFailed, err)
	}
	user, err := s.store.UpsertGoogleUser(ctx, currentUserID, identity.Subject, identity.Email, strings.TrimSpace(identity.Name))
	if err != nil {
		return domain.User{}, time.Time{}, err
	}
	return user, time.Now().UTC().Add(s.registeredTTL), nil
}

func (s *Service) GoogleCalendarURL(state, verifier string) (string, error) {
	if !s.GoogleEnabled() {
		return "", domain.ErrGoogleNotConfigured
	}
	return s.google.CalendarAuthURL(state, verifier), nil
}

func (s *Service) CompleteGoogleCalendar(ctx context.Context, userID, code, verifier string) error {
	if !s.GoogleEnabled() {
		return domain.ErrGoogleNotConfigured
	}
	refreshToken, scopes, err := s.google.ExchangeCalendar(ctx, code, verifier)
	if err != nil {
		return googleCalendarError(ctx, "connect", err)
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
		return GoogleCalendarOptions{}, googleCalendarError(ctx, "list", err)
	}
	importable := make([]googleclient.Calendar, 0, len(calendars))
	for _, calendar := range calendars {
		if !googleclient.IsHolidayCalendarID(calendar.ID) {
			importable = append(importable, calendar)
		}
	}
	return GoogleCalendarOptions{
		Calendars:    importable,
		WriteEnabled: hasGoogleCalendarScope(scopes, googleclient.CalendarEventsScope),
	}, nil
}

func (s *Service) PreviewGoogleCalendarAvailability(ctx context.Context, eventID, userID string, calendarIDs []string) ([]domain.AvailabilityInterval, error) {
	if !s.GoogleEnabled() {
		return nil, domain.ErrGoogleNotConfigured
	}
	// Older clients and saved preferences may still include holiday IDs.
	importableIDs := make([]string, 0, len(calendarIDs))
	for _, id := range calendarIDs {
		if !googleclient.IsHolidayCalendarID(id) {
			importableIDs = append(importableIDs, id)
		}
	}
	calendarIDs = importableIDs
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
	user, err := s.store.GetUser(ctx, userID)
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
	busy, err := s.google.FreeBusy(ctx, string(plain), calendarIDs, event.WindowStartAt, event.WindowEndAt, user.TimeZone)
	if err != nil {
		return nil, googleCalendarError(ctx, "freebusy", err)
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

func googleCalendarError(ctx context.Context, operation string, err error) error {
	var calendarError *googleclient.CalendarError
	if errors.As(err, &calendarError) {
		slog.WarnContext(ctx, "Google Calendar request failed", "operation", operation, "kind", calendarError.Kind, "provider_status", calendarError.Status)
		switch calendarError.Kind {
		case "reauthorization_required":
			return domain.ErrCalendarReconnectRequired
		case "permission_required":
			return domain.ErrCalendarReadPermission
		case "calendar_unavailable":
			return domain.ErrCalendarUnavailable
		case "api_disabled":
			return domain.ErrGoogleNotConfigured
		default:
			return domain.ErrCalendarTemporarilyUnavailable
		}
	}
	// Provider errors may contain credentials or personal data; do not log their text.
	slog.WarnContext(ctx, "Google Calendar request failed", "operation", operation, "kind", "unknown")
	return domain.ErrOAuthFailed
}
