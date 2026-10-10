package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
)

func eventCalendarEntry(publicAppURL string, event domain.Event, group *domain.SchedulingGroup, startAt, endAt time.Time) googleclient.CalendarEvent {
	groupID := ""
	summary := event.Title
	if group != nil {
		groupID = group.ID
		summary += "（" + group.Name + "）"
	}
	description := "vacaroで確定した予定です。"
	if publicAppURL != "" {
		description += "\n" + strings.TrimRight(publicAppURL, "/") + "/events/" + event.ID + "/confirmed"
	}
	return googleclient.CalendarEvent{
		ID:          confirmationCalendarEventID(event.ID, groupID),
		Summary:     summary,
		Description: description,
		StartAt:     startAt,
		EndAt:       endAt,
		TimeZone:    event.TimeZone,
	}
}

func confirmationCalendarEventID(eventID, groupID string) string {
	key := "event:" + eventID
	if groupID != "" {
		key += ":group:" + groupID
	}
	digest := sha256.Sum256([]byte(key))
	return "vacaro" + hex.EncodeToString(digest[:])
}

func (s *Service) syncConfirmationCalendars(ctx context.Context, participants []domain.Participant, event googleclient.CalendarEvent, eventID, groupID string) {
	seen := make(map[string]struct{}, len(participants))
	for _, participant := range participants {
		if _, exists := seen[participant.UserID]; exists {
			continue
		}
		seen[participant.UserID] = struct{}{}
		refreshToken, ok := s.googleCalendarTokenForWrite(ctx, participant.UserID, event.ID)
		if !ok {
			continue
		}
		if err := s.google.UpsertCalendarEvent(ctx, refreshToken, event); err != nil {
			logCalendarSyncFailure(ctx, "upsert calendar event", eventID, groupID, err)
		}
	}
}

func (s *Service) deleteConfirmationCalendars(ctx context.Context, participants []domain.Participant, calendarEventID, eventID, groupID string) {
	seen := make(map[string]struct{}, len(participants))
	for _, participant := range participants {
		if _, exists := seen[participant.UserID]; exists {
			continue
		}
		seen[participant.UserID] = struct{}{}
		refreshToken, ok := s.googleCalendarTokenForWrite(ctx, participant.UserID, calendarEventID)
		if !ok {
			continue
		}
		if err := s.google.DeleteCalendarEvent(ctx, refreshToken, calendarEventID); err != nil {
			logCalendarSyncFailure(ctx, "delete calendar event", eventID, groupID, err)
		}
	}
}

func (s *Service) googleCalendarTokenForWrite(ctx context.Context, userID, calendarEventID string) (string, bool) {
	ciphertext, scopes, err := s.store.GetGoogleCalendarConnection(ctx, userID)
	if errors.Is(err, domain.ErrCalendarNotConnected) {
		return "", false
	}
	if err != nil {
		slog.WarnContext(ctx, "Google Calendar connection lookup failed", "user_id", userID, "calendar_event_id", calendarEventID, "error", err)
		return "", false
	}
	if !hasGoogleCalendarScope(scopes, googleclient.CalendarEventsScope) {
		return "", false
	}
	plain, err := s.tokenBox.Open(ciphertext)
	if err != nil {
		slog.WarnContext(ctx, "Google Calendar token decryption failed", "user_id", userID, "calendar_event_id", calendarEventID, "error", err)
		return "", false
	}
	return string(plain), true
}

func hasGoogleCalendarScope(scopes []string, wanted string) bool {
	for _, scope := range scopes {
		if scope == wanted {
			return true
		}
	}
	return false
}

func logCalendarSyncFailure(ctx context.Context, operation, eventID, groupID string, err error) {
	attributes := []any{"operation", operation, "event_id", eventID, "error", err}
	if groupID != "" {
		attributes = append(attributes, "group_id", groupID)
	}
	slog.WarnContext(ctx, "Google Calendar confirmation sync failed", attributes...)
}
