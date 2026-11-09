package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/astalum/vacaro-back/internal/icloud"
)

func (s *Service) ConfigureICloud(provider ICloudProvider, box tokenBox) {
	s.icloud = provider
	s.icloudBox = box
}
func (s *Service) ICloudEnabled() bool { return s.icloud != nil && s.icloudBox != nil }
func (s *Service) ConnectICloud(ctx context.Context, userID, username, password string) error {
	if !s.ICloudEnabled() {
		return domain.ErrICloudNotConfigured
	}
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	address, err := mail.ParseAddress(username)
	if err != nil || address.Address != username || len(username) > 254 {
		return &ValidationError{Fields: map[string]string{"apple_account": "Apple Accountのメールアドレスを入力してください"}}
	}
	compact := strings.ReplaceAll(password, "-", "")
	valid := len(compact) == 16
	for _, ch := range compact {
		if ch < 'a' || ch > 'z' {
			valid = false
		}
	}
	if !valid {
		return &ValidationError{Fields: map[string]string{"app_password": "Appleのアプリ用パスワードを入力してください。通常のパスワードは入力しないでください"}}
	}
	if _, err := s.store.GetUser(ctx, userID); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cred, err := s.icloud.Discover(ctx, username, password, userID)
	if err != nil {
		return icloudError(ctx, "connect", err)
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	ciphertext, err := s.icloudBox.Seal(data)
	if err != nil {
		return err
	}
	if err := s.store.SaveICloudConnection(ctx, userID, ciphertext, cred.CalendarURL); err != nil {
		return err
	}
	return s.store.ClearAllICloudCalendarSyncs(ctx, userID)
}
func (s *Service) icloudCredentials(ctx context.Context, userID string) (icloud.Credentials, error) {
	if !s.ICloudEnabled() {
		return icloud.Credentials{}, domain.ErrICloudNotConfigured
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return icloud.Credentials{}, err
	}
	if user.GuestExpiresAt != nil && !time.Now().Before(*user.GuestExpiresAt) {
		return icloud.Credentials{}, domain.ErrICloudNotConnected
	}
	ciphertext, _, err := s.store.GetICloudConnection(ctx, userID)
	if err != nil {
		return icloud.Credentials{}, err
	}
	data, err := s.icloudBox.Open(ciphertext)
	if err != nil {
		return icloud.Credentials{}, domain.ErrICloudReconnectRequired
	}
	var cred icloud.Credentials
	if json.Unmarshal(data, &cred) != nil || cred.Username == "" || cred.Password == "" || cred.HomeURL == "" || cred.CalendarURL == "" {
		return cred, domain.ErrICloudReconnectRequired
	}
	return cred, nil
}

type ICloudCalendarOptions struct {
	Calendars []icloud.Calendar `json:"calendars"`
	Account   string            `json:"account"`
}

func (s *Service) ListICloudCalendars(ctx context.Context, userID string) (ICloudCalendarOptions, error) {
	cred, err := s.icloudCredentials(ctx, userID)
	if err != nil {
		return ICloudCalendarOptions{}, err
	}
	calendars, err := s.icloud.ListCalendars(ctx, cred)
	if err != nil {
		return ICloudCalendarOptions{}, icloudError(ctx, "list", err)
	}
	return ICloudCalendarOptions{Calendars: calendars, Account: cred.Username}, nil
}
func (s *Service) DisconnectICloud(ctx context.Context, userID string) error {
	if !s.ICloudEnabled() {
		return domain.ErrICloudNotConfigured
	}
	return s.store.DeleteICloudConnection(ctx, userID)
}
func (s *Service) PreviewICloudAvailability(ctx context.Context, eventID, userID string, ids []string) ([]domain.AvailabilityInterval, error) {
	if len(ids) > 50 {
		return nil, &ValidationError{Fields: map[string]string{"calendar_ids": "50件以下で指定してください"}}
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 2048 {
			return nil, &ValidationError{Fields: map[string]string{"calendar_ids": "カレンダーを選び直してください"}}
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
	cred, err := s.icloudCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	busy, err := s.icloud.Busy(ctx, cred, ids, event.WindowStartAt, event.WindowEndAt, user.TimeZone)
	if err != nil {
		return nil, icloudError(ctx, "availability", err)
	}
	free := subtractBusy(event.WindowStartAt, event.WindowEndAt, busy)
	for i := range free {
		free[i].Source = "icloud_calendar"
	}
	return free, nil
}
func icloudError(ctx context.Context, operation string, err error) error {
	var providerError *icloud.Error
	kind := "unknown"
	status := 0
	if errors.As(err, &providerError) {
		kind = providerError.Kind
		status = providerError.Status
	}
	slog.WarnContext(ctx, "iCloud Calendar request failed", "operation", operation, "kind", kind, "provider_status", status)
	switch kind {
	case "reconnect_required":
		return domain.ErrICloudReconnectRequired
	case "permission_required":
		return domain.ErrICloudPermission
	case "calendar_unavailable":
		return domain.ErrICloudUnavailable
	case "conflict":
		return domain.ErrICloudConflict
	default:
		return domain.ErrICloudTemporarilyUnavailable
	}
}

// Every sync failure is recorded independently of the Google provider and never
// rolls back a successfully confirmed Vacaro schedule.
func (s *Service) syncICloudCalendarForUser(ctx context.Context, eventID string, groupID *string, userID string, event googleclient.CalendarEvent) (domain.ConfirmationCalendarSync, error) {
	cred, err := s.icloudCredentials(ctx, userID)
	if err != nil {
		return domain.ConfirmationCalendarSync{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	created, writeErr := s.icloud.UpsertEvent(ctx, cred, event)
	if created {
		if err := s.store.ClearAllICloudCalendarSyncs(ctx, userID); err != nil {
			return domain.ConfirmationCalendarSync{}, err
		}
	}
	if err := writeErr; err != nil {
		_, _ = s.store.UpsertICloudCalendarSync(ctx, eventID, groupID, userID, cred.CalendarURL, "failed")
		_ = icloudError(ctx, "sync", err)
		return domain.ConfirmationCalendarSync{}, domain.ErrICloudSyncFailed
	}
	return s.store.UpsertICloudCalendarSync(ctx, eventID, groupID, userID, cred.CalendarURL, "synced")
}
func (s *Service) syncICloudCalendars(ctx context.Context, participants []domain.Participant, event googleclient.CalendarEvent, eventID, groupID string) {
	seen := map[string]bool{}
	for _, participant := range participants {
		if seen[participant.UserID] {
			continue
		}
		seen[participant.UserID] = true
		if _, err := s.syncICloudCalendarForUser(ctx, eventID, optionalGroupID(groupID), participant.UserID, event); err != nil && !errors.Is(err, domain.ErrICloudNotConnected) {
			slog.WarnContext(ctx, "iCloud confirmation sync failed", "event_id", eventID, "group_id", groupID)
		}
	}
}
func (s *Service) deleteICloudCalendars(ctx context.Context, participants []domain.Participant, id, eventID, groupID string) {
	seen := map[string]bool{}
	for _, participant := range participants {
		if seen[participant.UserID] {
			continue
		}
		seen[participant.UserID] = true
		cred, err := s.icloudCredentials(ctx, participant.UserID)
		if err != nil {
			continue
		}
		if err := s.icloud.DeleteEvent(ctx, cred, id); err != nil {
			_ = icloudError(ctx, "delete", err)
		}
	}
	if err := s.store.ClearICloudCalendarSync(ctx, eventID, optionalGroupID(groupID)); err != nil {
		slog.WarnContext(ctx, "iCloud sync status cleanup failed", "event_id", eventID, "group_id", groupID)
	}
}
func (s *Service) GetICloudCalendarStatus(ctx context.Context, eventID, userID string) (ConfirmationCalendarStatus, error) {
	if _, err := s.store.GetEventForUser(ctx, eventID, userID); err != nil {
		return ConfirmationCalendarStatus{}, err
	}
	result := ConfirmationCalendarStatus{ConnectionStatus: "unavailable", Syncs: []domain.ConfirmationCalendarSync{}}
	if !s.ICloudEnabled() {
		return result, nil
	}
	if _, err := s.icloudCredentials(ctx, userID); err != nil {
		if errors.Is(err, domain.ErrICloudNotConnected) {
			result.ConnectionStatus = "not_connected"
		} else {
			result.ConnectionStatus = "error"
		}
		return result, nil
	}
	syncs, err := s.store.ListICloudCalendarSyncs(ctx, eventID, userID)
	if err != nil {
		return result, err
	}
	result.ConnectionStatus = "connected"
	result.Syncs = syncs
	return result, nil
}
func (s *Service) SyncOwnICloudCalendar(ctx context.Context, eventID string, groupID *string, userID string) (domain.ConfirmationCalendarSync, error) {
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
	return s.syncICloudCalendarForUser(ctx, eventID, groupID, userID, entry)
}
