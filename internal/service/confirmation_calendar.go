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

func (s *Service) deleteConfirmationCalendarsUnlocked(ctx context.Context, participants []domain.Participant, calendarEventID, eventID, groupID string) {
	seen := make(map[string]struct{}, len(participants))
	for _, participant := range participants {
		if _, exists := seen[participant.UserID]; exists {
			continue
		}
		seen[participant.UserID] = struct{}{}
		refreshToken, err := s.googleCalendarTokenForWrite(ctx, participant.UserID)
		if err != nil {
			if !errors.Is(err, domain.ErrCalendarNotConnected) && !errors.Is(err, domain.ErrCalendarWritePermission) {
				logCalendarSyncFailure(ctx, "load calendar connection", eventID, groupID, err)
			}
			continue
		}
		destination, err := s.store.GetGoogleCalendarDestination(ctx, participant.UserID)
		if err != nil {
			logCalendarSyncFailure(ctx, "load calendar destination", eventID, groupID, err)
			continue
		}
		// Deletion never provisions a calendar and never falls back to primary.
		if destination == "" {
			continue
		}
		if err := s.google.DeleteCalendarEvent(ctx, refreshToken, destination, calendarEventID); err != nil {
			logCalendarSyncFailure(ctx, "delete calendar event", eventID, groupID, err)
		}
	}
}

func (s *Service) googleCalendarTokenForWrite(ctx context.Context, userID string) (string, error) {
	ciphertext, scopes, err := s.store.GetGoogleCalendarConnection(ctx, userID)
	if err != nil {
		return "", err
	}
	if !hasGoogleCalendarScope(scopes, googleclient.CalendarAppCreatedScope) {
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
	if err := s.requireCalendarProvider(ctx, userID, "google", false); err != nil {
		if errors.Is(err, domain.ErrInactiveCalendar) {
			return ConfirmationCalendarStatus{ConnectionStatus: "inactive", Syncs: []domain.ConfirmationCalendarSync{}}, nil
		}
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
	if !hasGoogleCalendarScope(scopes, googleclient.CalendarAppCreatedScope) {
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

func (s *Service) syncConfirmationCalendarForUserUnlocked(ctx context.Context, eventID string, groupID *string, userID string, event googleclient.CalendarEvent) (domain.ConfirmationCalendarSync, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return domain.ConfirmationCalendarSync{}, err
	}
	event.TimeZone = user.TimeZone
	refreshToken, err := s.googleCalendarTokenForWrite(ctx, userID)
	if err != nil {
		if !errors.Is(err, domain.ErrCalendarNotConnected) && !errors.Is(err, domain.ErrCalendarWritePermission) {
			_, _ = s.store.UpsertConfirmationCalendarSync(ctx, eventID, groupID, userID, event.CalendarID, "failed")
			return domain.ConfirmationCalendarSync{}, fmt.Errorf("%w: %v", domain.ErrCalendarSyncFailed, err)
		}
		return domain.ConfirmationCalendarSync{}, err
	}
	event.CalendarID, err = s.ensureGoogleCalendarDestination(ctx, userID, refreshToken, user.TimeZone)
	if err != nil {
		_, _ = s.store.UpsertConfirmationCalendarSync(ctx, eventID, groupID, userID, "", "failed")
		return domain.ConfirmationCalendarSync{}, fmt.Errorf("%w: %v", domain.ErrCalendarSyncFailed, err)
	}
	if err := s.google.UpsertCalendarEvent(ctx, refreshToken, event); err != nil {
		_, _ = s.store.UpsertConfirmationCalendarSync(ctx, eventID, groupID, userID, event.CalendarID, "failed")
		return domain.ConfirmationCalendarSync{}, fmt.Errorf("%w: %v", domain.ErrCalendarSyncFailed, err)
	}
	return s.store.UpsertConfirmationCalendarSync(ctx, eventID, groupID, userID, event.CalendarID, "synced")
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

// Provisioning is serialized in PostgreSQL for each user, including the first
// connection and requests handled by different API instances.
func (s *Service) ensureGoogleCalendarDestination(ctx context.Context, userID, refreshToken, timeZone string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.store.ResolveGoogleCalendarDestination(ctx, userID, func(current string) (string, error) {
		return s.google.EnsureCalendar(ctx, refreshToken, current, timeZone)
	})
}

func (s *Service) syncConfirmationCalendarForUser(ctx context.Context, eventID string, groupID *string, userID string, event googleclient.CalendarEvent) (domain.ConfirmationCalendarSync, error) {
	return calendarResult(s, ctx, userID, "google", false, func() (domain.ConfirmationCalendarSync, error) {
		return s.syncConfirmationCalendarForUserUnlocked(ctx, eventID, groupID, userID, event)
	})
}

func (s *Service) deleteConfirmationCalendars(ctx context.Context, participants []domain.Participant, id, eventID, groupID string) {
	seen := map[string]bool{}
	for _, p := range participants {
		if seen[p.UserID] {
			continue
		}
		seen[p.UserID] = true
		err := s.withActiveCalendar(ctx, p.UserID, "google", false, func() error {
			s.deleteConfirmationCalendarsUnlocked(ctx, []domain.Participant{p}, id, eventID, groupID)
			return nil
		})
		if err != nil && !errors.Is(err, domain.ErrInactiveCalendar) {
			logCalendarSyncFailure(ctx, "load selected calendar", eventID, groupID, err)
		}
	}
}
