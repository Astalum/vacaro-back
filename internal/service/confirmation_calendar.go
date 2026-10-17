package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
		TimeZone:    "UTC",
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
		if _, err := s.syncConfirmationCalendarForUser(ctx, eventID, optionalGroupID(groupID), participant.UserID, event); err != nil {
			if errors.Is(err, domain.ErrCalendarNotConnected) || errors.Is(err, domain.ErrCalendarWritePermission) {
				continue
			}
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
		refreshToken, err := s.googleCalendarTokenForWrite(ctx, participant.UserID, calendarEventID)
		if err != nil {
			if !errors.Is(err, domain.ErrCalendarNotConnected) && !errors.Is(err, domain.ErrCalendarWritePermission) {
				logCalendarSyncFailure(ctx, "load calendar connection", eventID, groupID, err)
			}
			continue
		}
		if err := s.google.DeleteCalendarEvent(ctx, refreshToken, calendarEventID); err != nil {
			logCalendarSyncFailure(ctx, "delete calendar event", eventID, groupID, err)
		}
	}
}

func (s *Service) googleCalendarTokenForWrite(ctx context.Context, userID, calendarEventID string) (string, error) {
	ciphertext, scopes, err := s.store.GetGoogleCalendarConnection(ctx, userID)
	if err != nil {
		return "", err
	}
	if !hasGoogleCalendarScope(scopes, googleclient.CalendarEventsScope) {
		return "", domain.ErrCalendarWritePermission
	}
	plain, err := s.tokenBox.Open(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

type ConfirmationCalendarStatus struct {
	ConnectionStatus string                            `json:"connection_status"`
	Syncs            []domain.ConfirmationCalendarSync `json:"syncs"`
}

func (s *Service) GetConfirmationCalendarStatus(ctx context.Context, eventID, userID string) (ConfirmationCalendarStatus, error) {
	if _, err := s.store.GetEventForUser(ctx, eventID, userID); err != nil {
		return ConfirmationCalendarStatus{}, err
	}
	syncs, err := s.store.ListConfirmationCalendarSyncs(ctx, eventID, userID)
	if err != nil {
		return ConfirmationCalendarStatus{}, err
	}
	result := ConfirmationCalendarStatus{ConnectionStatus: "unavailable", Syncs: syncs}
	if !s.GoogleEnabled() {
		return result, nil
	}
	_, scopes, err := s.store.GetGoogleCalendarConnection(ctx, userID)
	if errors.Is(err, domain.ErrCalendarNotConnected) {
		result.ConnectionStatus = "not_connected"
		return result, nil
	}
	if err != nil {
		slog.WarnContext(ctx, "Google Calendar connection status lookup failed", "user_id", userID, "event_id", eventID, "error", err)
		result.ConnectionStatus = "error"
		return result, nil
	}
	if !hasGoogleCalendarScope(scopes, googleclient.CalendarEventsScope) {
		result.ConnectionStatus = "write_permission_required"
		return result, nil
	}
	result.ConnectionStatus = "connected"
	return result, nil
}

func (s *Service) SyncOwnConfirmationCalendar(ctx context.Context, eventID string, groupID *string, userID string) (domain.ConfirmationCalendarSync, error) {
	if !s.GoogleEnabled() {
		return domain.ConfirmationCalendarSync{}, domain.ErrGoogleNotConfigured
	}
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return domain.ConfirmationCalendarSync{}, err
	}
	if event.Status == "cancelled" {
		return domain.ConfirmationCalendarSync{}, domain.ErrInvalidTransition
	}
	var entry googleclient.CalendarEvent
	if groupID == nil {
		if event.ConfirmedStartAt == nil || event.ConfirmedEndAt == nil {
			return domain.ConfirmationCalendarSync{}, domain.ErrInvalidTransition
		}
		entry = eventCalendarEntry(s.publicAppURL, event, nil, *event.ConfirmedStartAt, *event.ConfirmedEndAt)
	} else {
		if event.SchedulingMode != "groups" {
			return domain.ConfirmationCalendarSync{}, domain.ErrGroupNotEditable
		}
		group, err := s.store.GetGroup(ctx, eventID, *groupID, userID)
		if err != nil {
			return domain.ConfirmationCalendarSync{}, err
		}
		if !groupContainsUser(group, userID) {
			return domain.ConfirmationCalendarSync{}, domain.ErrPermissionDenied
		}
		if group.ConfirmedStartAt == nil || group.ConfirmedEndAt == nil {
			return domain.ConfirmationCalendarSync{}, domain.ErrInvalidTransition
		}
		entry = eventCalendarEntry(s.publicAppURL, event, &group, *group.ConfirmedStartAt, *group.ConfirmedEndAt)
	}
	return s.syncConfirmationCalendarForUser(ctx, eventID, groupID, userID, entry)
}

func (s *Service) syncConfirmationCalendarForUser(ctx context.Context, eventID string, groupID *string, userID string, event googleclient.CalendarEvent) (domain.ConfirmationCalendarSync, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return domain.ConfirmationCalendarSync{}, err
	}
	event.TimeZone = user.TimeZone
	refreshToken, err := s.googleCalendarTokenForWrite(ctx, userID, event.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrCalendarNotConnected) && !errors.Is(err, domain.ErrCalendarWritePermission) {
			_, _ = s.store.UpsertConfirmationCalendarSync(ctx, eventID, groupID, userID, "failed")
			return domain.ConfirmationCalendarSync{}, fmt.Errorf("%w: %v", domain.ErrCalendarSyncFailed, err)
		}
		return domain.ConfirmationCalendarSync{}, err
	}
	if err := s.google.UpsertCalendarEvent(ctx, refreshToken, event); err != nil {
		_, _ = s.store.UpsertConfirmationCalendarSync(ctx, eventID, groupID, userID, "failed")
		return domain.ConfirmationCalendarSync{}, fmt.Errorf("%w: %v", domain.ErrCalendarSyncFailed, err)
	}
	return s.store.UpsertConfirmationCalendarSync(ctx, eventID, groupID, userID, "synced")
}

func groupContainsUser(group domain.SchedulingGroup, userID string) bool {
	for _, member := range group.Members {
		if member.UserID == userID {
			return true
		}
	}
	return false
}

func optionalGroupID(groupID string) *string {
	if groupID == "" {
		return nil
	}
	return &groupID
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
