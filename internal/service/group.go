package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/astalum/vacaro-back/internal/repository"
)

type CreateGroupInput struct {
	Name            string
	DurationMinutes *int
	ParticipantIDs  []string
}

type UpdateGroupInput struct {
	Name            *string
	DurationMinutes *int
}

func (s *Service) ListGroups(ctx context.Context, eventID, userID string) ([]domain.SchedulingGroup, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return nil, err
	}
	if event.SchedulingMode != "groups" {
		return nil, domain.ErrGroupNotEditable
	}
	return s.store.ListGroups(ctx, eventID, userID)
}

func (s *Service) GetGroup(ctx context.Context, eventID, groupID, userID string) (domain.SchedulingGroup, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	if event.SchedulingMode != "groups" {
		return domain.SchedulingGroup{}, domain.ErrGroupNotEditable
	}
	return s.store.GetGroup(ctx, eventID, groupID, userID)
}

func (s *Service) CreateGroup(ctx context.Context, eventID, userID string, input CreateGroupInput) (domain.SchedulingGroup, error) {
	event, err := s.requireGroupOwner(ctx, eventID, userID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	name := strings.TrimSpace(input.Name)
	duration := event.DurationMinutes
	if input.DurationMinutes != nil {
		duration = *input.DurationMinutes
	}
	fields := validateGroupFields(name, duration, event)
	if err := s.validateGroupMembers(ctx, eventID, userID, input.ParticipantIDs, fields); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if len(fields) > 0 {
		return domain.SchedulingGroup{}, &ValidationError{Fields: fields}
	}
	return s.store.CreateGroup(ctx, repository.CreateGroupParams{
		EventID: eventID, UserID: userID, Name: name, DurationMinutes: duration,
		ParticipantIDs: input.ParticipantIDs,
	})
}

func (s *Service) UpdateGroup(ctx context.Context, eventID, groupID, userID string, input UpdateGroupInput) (domain.SchedulingGroup, error) {
	event, err := s.requireGroupOwner(ctx, eventID, userID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	group, err := s.store.GetGroup(ctx, eventID, groupID, userID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	name, duration := group.Name, group.DurationMinutes
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	if input.DurationMinutes != nil {
		duration = *input.DurationMinutes
	}
	if fields := validateGroupFields(name, duration, event); len(fields) > 0 {
		return domain.SchedulingGroup{}, &ValidationError{Fields: fields}
	}
	return s.store.UpdateGroup(ctx, repository.UpdateGroupParams{
		EventID: eventID, GroupID: groupID, UserID: userID, Name: name, DurationMinutes: duration,
	})
}

func (s *Service) DeleteGroup(ctx context.Context, eventID, groupID, userID string) error {
	if _, err := s.requireGroupOwner(ctx, eventID, userID); err != nil {
		return err
	}
	return s.store.DeleteGroup(ctx, eventID, groupID, userID)
}

func (s *Service) ReplaceGroupMembers(ctx context.Context, eventID, groupID, userID string, participantIDs []string) (domain.SchedulingGroup, error) {
	if _, err := s.requireGroupOwner(ctx, eventID, userID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if _, err := s.store.GetGroup(ctx, eventID, groupID, userID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	fields := map[string]string{}
	if err := s.validateGroupMembers(ctx, eventID, userID, participantIDs, fields); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if len(fields) > 0 {
		return domain.SchedulingGroup{}, &ValidationError{Fields: fields}
	}
	return s.store.ReplaceGroupMembers(ctx, eventID, groupID, userID, participantIDs)
}

func (s *Service) ListGroupAvailability(ctx context.Context, eventID, groupID, userID string) ([]domain.ParticipantAvailability, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return nil, err
	}
	if event.SchedulingMode != "groups" {
		return nil, domain.ErrGroupNotEditable
	}
	if !canViewResults(event, userID) {
		return nil, domain.ErrPermissionDenied
	}
	return s.store.ListGroupAvailability(ctx, eventID, groupID, userID)
}

func (s *Service) GroupCandidates(ctx context.Context, eventID, groupID, userID string, limit int) ([]domain.Candidate, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return nil, err
	}
	if event.SchedulingMode != "groups" {
		return nil, domain.ErrGroupNotEditable
	}
	if !canViewResults(event, userID) {
		return nil, domain.ErrPermissionDenied
	}
	group, err := s.store.GetGroup(ctx, eventID, groupID, userID)
	if err != nil {
		return nil, err
	}
	availability, err := s.store.ListGroupAvailability(ctx, eventID, groupID, userID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	event.DurationMinutes = group.DurationMinutes
	return calculateCandidates(event, availability, limit), nil
}

func (s *Service) ConfirmGroup(ctx context.Context, eventID, groupID, userID string, startAt, endAt time.Time) (domain.SchedulingGroup, error) {
	event, err := s.requireGroupConfirmationOwner(ctx, eventID, userID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	group, err := s.store.GetGroup(ctx, eventID, groupID, userID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	startAt, endAt = startAt.UTC(), endAt.UTC()
	fields := map[string]string{}
	if len(group.Members) == 0 {
		fields["group_id"] = "1人以上のメンバーを割り当ててください"
	}
	if !startAt.Before(endAt) {
		fields["end_at"] = "start_atより後を指定してください"
	}
	if startAt.Before(event.WindowStartAt) || endAt.After(event.WindowEndAt) {
		fields["start_at"] = "イベント期間内を指定してください"
	}
	if endAt.Sub(startAt) != time.Duration(group.DurationMinutes)*time.Minute {
		fields["end_at"] = "グループの必要時間と一致させてください"
	}
	if len(fields) > 0 {
		return domain.SchedulingGroup{}, &ValidationError{Fields: fields}
	}
	confirmed, err := s.store.ConfirmGroup(ctx, eventID, groupID, userID, startAt, endAt)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	if err := s.store.ClearConfirmationCalendarSync(ctx, eventID, &groupID); err != nil {
		logCalendarSyncFailure(ctx, "clear calendar sync status", eventID, groupID, err)
	}
	if s.GoogleEnabled() {
		s.syncConfirmationCalendars(ctx, confirmed.Members, eventCalendarEntry(s.publicAppURL, event, &confirmed, startAt, endAt), eventID, groupID)
	}
	return confirmed, nil
}

func (s *Service) requireGroupConfirmationOwner(ctx context.Context, eventID, userID string) (domain.Event, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return domain.Event{}, err
	}
	if event.CreatedByUserID != userID {
		return domain.Event{}, domain.ErrPermissionDenied
	}
	if event.Status == "cancelled" || event.SchedulingMode != "groups" {
		return domain.Event{}, domain.ErrGroupNotEditable
	}
	return event, nil
}

func (s *Service) ReopenGroup(ctx context.Context, eventID, groupID, userID string) (domain.SchedulingGroup, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	if event.CreatedByUserID != userID {
		return domain.SchedulingGroup{}, domain.ErrPermissionDenied
	}
	if event.SchedulingMode != "groups" {
		return domain.SchedulingGroup{}, domain.ErrGroupNotEditable
	}
	reopened, err := s.store.ReopenGroup(ctx, eventID, groupID, userID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	if err := s.store.ClearConfirmationCalendarSync(ctx, eventID, &groupID); err != nil {
		logCalendarSyncFailure(ctx, "clear calendar sync status", eventID, groupID, err)
	}
	if s.GoogleEnabled() {
		s.deleteConfirmationCalendars(ctx, reopened.Members, confirmationCalendarEventID(eventID, groupID), eventID, groupID)
	}
	return reopened, nil
}

func (s *Service) requireGroupOwner(ctx context.Context, eventID, userID string) (domain.Event, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return domain.Event{}, err
	}
	if event.CreatedByUserID != userID {
		return domain.Event{}, domain.ErrPermissionDenied
	}
	if event.Status != "open" || event.SchedulingMode != "groups" {
		return domain.Event{}, domain.ErrGroupNotEditable
	}
	return event, nil
}

func (s *Service) validateGroupMembers(ctx context.Context, eventID, userID string, participantIDs []string, fields map[string]string) error {
	seen := make(map[string]struct{}, len(participantIDs))
	for _, id := range participantIDs {
		if id == "" {
			fields["participant_ids"] = "参加者IDを指定してください"
			continue
		}
		if _, exists := seen[id]; exists {
			fields["participant_ids"] = "同じ参加者を重複して指定できません"
		}
		seen[id] = struct{}{}
	}
	participants, err := s.store.ListParticipants(ctx, eventID, userID)
	if err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(participants))
	for _, participant := range participants {
		allowed[participant.ID] = struct{}{}
	}
	for _, id := range participantIDs {
		if _, exists := allowed[id]; !exists {
			fields["participant_ids"] = "このイベントの参加者だけを指定してください"
		}
	}
	return nil
}

func validateGroupFields(name string, duration int, event domain.Event) map[string]string {
	fields := map[string]string{}
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		fields["name"] = "1～100文字で指定してください"
	}
	if duration < 5 || duration > 1440 {
		fields["duration_minutes"] = "5～1440分で指定してください"
	}
	if event.WindowEndAt.Sub(event.WindowStartAt) < time.Duration(duration)*time.Minute {
		fields["duration_minutes"] = "イベント期間以下にしてください"
	}
	return fields
}
