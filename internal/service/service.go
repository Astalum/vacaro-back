package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/astalum/vacaro-back/internal/domain"
	googleclient "github.com/astalum/vacaro-back/internal/google"
	"github.com/astalum/vacaro-back/internal/repository"
)

type Service struct {
	store         Store
	guestTTL      time.Duration
	publicAppURL  string
	google        GoogleProvider
	tokenBox      tokenBox
	registeredTTL time.Duration
}

type tokenBox interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}

type GoogleProvider interface {
	LoginAuthURL(string, string) string
	ExchangeIdentity(context.Context, string, string) (googleclient.Identity, error)
	CalendarAuthURL(string) string
	ExchangeCalendar(context.Context, string) (string, []string, error)
	ListCalendars(context.Context, string) ([]googleclient.Calendar, error)
	FreeBusy(context.Context, string, []string, time.Time, time.Time, string) ([]googleclient.BusyInterval, error)
	UpsertCalendarEvent(context.Context, string, googleclient.CalendarEvent) error
	DeleteCalendarEvent(context.Context, string, string) error
	Revoke(context.Context, string) error
}

type Store interface {
	CreateGuest(context.Context, string, time.Time) (domain.User, error)
	GetUser(context.Context, string) (domain.User, error)
	UpdateUserName(context.Context, string, string) (domain.User, error)
	CreateEvent(context.Context, repository.CreateEventParams) (domain.Event, error)
	ListEvents(context.Context, string, string, *time.Time, *string, int) ([]domain.Event, error)
	GetEventForUser(context.Context, string, string) (domain.Event, error)
	GetEventByInviteHash(context.Context, string) (domain.Event, error)
	UpdateEvent(context.Context, repository.UpdateEventParams) (domain.Event, error)
	RotateInviteToken(context.Context, string, string, string) error
	JoinEvent(context.Context, string, string, string) (domain.Participant, error)
	ListParticipants(context.Context, string, string) ([]domain.Participant, error)
	LeaveEvent(context.Context, string, string) error
	ReplaceAvailability(context.Context, string, string, []domain.AvailabilityInterval) ([]domain.AvailabilityInterval, error)
	GetOwnAvailability(context.Context, string, string) ([]domain.AvailabilityInterval, string, error)
	ListAvailability(context.Context, string, string) ([]domain.ParticipantAvailability, error)
	ConfirmEvent(context.Context, string, string, time.Time, time.Time) (domain.Event, error)
	ReopenEvent(context.Context, string, string) (domain.Event, error)
	CancelEvent(context.Context, string, string) error
	ListGroups(context.Context, string, string) ([]domain.SchedulingGroup, error)
	GetGroup(context.Context, string, string, string) (domain.SchedulingGroup, error)
	CreateGroup(context.Context, repository.CreateGroupParams) (domain.SchedulingGroup, error)
	UpdateGroup(context.Context, repository.UpdateGroupParams) (domain.SchedulingGroup, error)
	DeleteGroup(context.Context, string, string, string) error
	ReplaceGroupMembers(context.Context, string, string, string, []string) (domain.SchedulingGroup, error)
	ListGroupAvailability(context.Context, string, string, string) ([]domain.ParticipantAvailability, error)
	ConfirmGroup(context.Context, string, string, string, time.Time, time.Time) (domain.SchedulingGroup, error)
	ReopenGroup(context.Context, string, string, string) (domain.SchedulingGroup, error)
	UpsertGoogleUser(context.Context, string, string, string, string) (domain.User, error)
	SaveGoogleCalendarConnection(context.Context, string, []byte, []string) error
	GetGoogleCalendarConnection(context.Context, string) ([]byte, []string, error)
	DeleteGoogleCalendarConnection(context.Context, string) ([]byte, error)
	HasGoogleAccount(context.Context, string) (bool, error)
}

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "validation failed" }

func New(store Store, guestTTL time.Duration, publicAppURL string) *Service {
	return &Service{store: store, guestTTL: guestTTL, publicAppURL: strings.TrimRight(publicAppURL, "/")}
}

func (s *Service) ConfigureGoogle(provider GoogleProvider, box tokenBox, registeredTTL time.Duration) {
	s.google = provider
	s.tokenBox = box
	s.registeredTTL = registeredTTL
}

func (s *Service) CreateGuest(ctx context.Context, displayName string) (domain.User, error) {
	displayName = strings.TrimSpace(displayName)
	if err := validateDisplayName(displayName); err != nil {
		return domain.User{}, err
	}
	return s.store.CreateGuest(ctx, displayName, time.Now().UTC().Add(s.guestTTL))
}

func (s *Service) GetUser(ctx context.Context, userID string) (domain.User, error) {
	return s.store.GetUser(ctx, userID)
}

func (s *Service) UpdateUserName(ctx context.Context, userID, displayName string) (domain.User, error) {
	displayName = strings.TrimSpace(displayName)
	if err := validateDisplayName(displayName); err != nil {
		return domain.User{}, err
	}
	return s.store.UpdateUserName(ctx, userID, displayName)
}

type CreateEventInput struct {
	Title              string
	Description        *string
	TimeZone           string
	WindowStartAt      time.Time
	WindowEndAt        time.Time
	DurationMinutes    int
	SchedulingMode     string
	ResponseDeadlineAt *time.Time
}

type EventWithInvite struct {
	domain.Event
	InviteToken string `json:"invite_token"`
	InviteURL   string `json:"invite_url"`
}

func (s *Service) CreateEvent(ctx context.Context, userID string, input CreateEventInput) (EventWithInvite, error) {
	input = normalizeEvent(input)
	if err := validateEvent(input); err != nil {
		return EventWithInvite{}, err
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return EventWithInvite{}, err
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return EventWithInvite{}, err
	}
	event, err := s.store.CreateEvent(ctx, repository.CreateEventParams{
		CreatedByUserID: userID, InviteTokenHash: tokenHash, Title: input.Title,
		Description: input.Description, TimeZone: input.TimeZone, WindowStartAt: input.WindowStartAt,
		WindowEndAt: input.WindowEndAt, DurationMinutes: input.DurationMinutes, SchedulingMode: input.SchedulingMode,
		ResponseDeadlineAt: input.ResponseDeadlineAt, OrganizerName: user.DisplayName,
	})
	if err != nil {
		return EventWithInvite{}, err
	}
	return EventWithInvite{Event: event, InviteToken: token, InviteURL: s.publicAppURL + "/invitations/" + token}, nil
}

type EventPage struct {
	Events     []domain.Event
	NextCursor *string
}

type eventCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func (s *Service) ListEvents(ctx context.Context, userID, status, encodedCursor string, limit int) (EventPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if status != "" && status != "open" && status != "confirmed" && status != "cancelled" {
		return EventPage{}, &ValidationError{Fields: map[string]string{"status": "open、confirmed、cancelledのいずれかを指定してください"}}
	}
	var cursorTime *time.Time
	var cursorID *string
	if encodedCursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(encodedCursor)
		if err != nil {
			return EventPage{}, &ValidationError{Fields: map[string]string{"cursor": "有効なカーソルを指定してください"}}
		}
		var cursor eventCursor
		if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.CreatedAt.IsZero() || cursor.ID == "" {
			return EventPage{}, &ValidationError{Fields: map[string]string{"cursor": "有効なカーソルを指定してください"}}
		}
		cursorTime, cursorID = &cursor.CreatedAt, &cursor.ID
	}
	events, err := s.store.ListEvents(ctx, userID, status, cursorTime, cursorID, limit+1)
	if err != nil {
		return EventPage{}, err
	}
	var nextCursor *string
	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		payload, err := json.Marshal(eventCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return EventPage{}, err
		}
		value := base64.RawURLEncoding.EncodeToString(payload)
		nextCursor = &value
	}
	return EventPage{Events: events, NextCursor: nextCursor}, nil
}

func (s *Service) GetEvent(ctx context.Context, eventID, userID string) (domain.Event, error) {
	return s.store.GetEventForUser(ctx, eventID, userID)
}

func (s *Service) GetInvitation(ctx context.Context, token string) (domain.Event, error) {
	if token == "" {
		return domain.Event{}, domain.ErrNotFound
	}
	return s.store.GetEventByInviteHash(ctx, hashToken(token))
}

type UpdateEventInput struct {
	Title              *string
	Description        *string
	DescriptionSet     bool
	TimeZone           *string
	WindowStartAt      *time.Time
	WindowEndAt        *time.Time
	DurationMinutes    *int
	SchedulingMode     *string
	ResponseDeadlineAt *time.Time
	DeadlineSet        bool
}

func (s *Service) UpdateEvent(ctx context.Context, eventID, userID string, input UpdateEventInput) (domain.Event, error) {
	current, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return domain.Event{}, err
	}
	if current.CreatedByUserID != userID {
		return domain.Event{}, domain.ErrPermissionDenied
	}
	if current.Status != "open" {
		return domain.Event{}, domain.ErrEventNotOpen
	}
	merged := CreateEventInput{
		Title: current.Title, Description: current.Description, TimeZone: current.TimeZone,
		WindowStartAt: current.WindowStartAt, WindowEndAt: current.WindowEndAt,
		DurationMinutes: current.DurationMinutes, ResponseDeadlineAt: current.ResponseDeadlineAt,
		SchedulingMode: current.SchedulingMode,
	}
	if input.Title != nil {
		merged.Title = *input.Title
	}
	if input.DescriptionSet {
		merged.Description = input.Description
	}
	if input.TimeZone != nil {
		merged.TimeZone = *input.TimeZone
	}
	if input.WindowStartAt != nil {
		merged.WindowStartAt = *input.WindowStartAt
	}
	if input.WindowEndAt != nil {
		merged.WindowEndAt = *input.WindowEndAt
	}
	if input.DurationMinutes != nil {
		merged.DurationMinutes = *input.DurationMinutes
	}
	if input.SchedulingMode != nil {
		merged.SchedulingMode = *input.SchedulingMode
	}
	if input.DeadlineSet {
		merged.ResponseDeadlineAt = input.ResponseDeadlineAt
	}
	merged = normalizeEvent(merged)
	if err := validateEvent(merged); err != nil {
		return domain.Event{}, err
	}
	availability, err := s.store.ListAvailability(ctx, eventID, userID)
	if err != nil {
		return domain.Event{}, err
	}
	for _, participant := range availability {
		for _, interval := range participant.Intervals {
			if interval.StartAt.Before(merged.WindowStartAt) || interval.EndAt.After(merged.WindowEndAt) {
				return domain.Event{}, &ValidationError{Fields: map[string]string{
					"window_start_at": "登録済みの参加可能時間を含む期間を指定してください",
				}}
			}
		}
	}
	return s.store.UpdateEvent(ctx, repository.UpdateEventParams{
		EventID: eventID, UserID: userID, Title: merged.Title, Description: merged.Description,
		TimeZone: merged.TimeZone, WindowStartAt: merged.WindowStartAt, WindowEndAt: merged.WindowEndAt,
		DurationMinutes: merged.DurationMinutes, SchedulingMode: merged.SchedulingMode,
		ResponseDeadlineAt: merged.ResponseDeadlineAt,
	})
}

func (s *Service) RotateInviteToken(ctx context.Context, eventID, userID string) (EventWithInvite, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return EventWithInvite{}, err
	}
	if event.CreatedByUserID != userID {
		return EventWithInvite{}, domain.ErrPermissionDenied
	}
	if event.Status != "open" {
		return EventWithInvite{}, domain.ErrEventNotOpen
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return EventWithInvite{}, err
	}
	if err := s.store.RotateInviteToken(ctx, eventID, userID, tokenHash); err != nil {
		return EventWithInvite{}, err
	}
	event, err = s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return EventWithInvite{}, err
	}
	return EventWithInvite{Event: event, InviteToken: token, InviteURL: s.publicAppURL + "/invitations/" + token}, nil
}

func (s *Service) JoinEvent(ctx context.Context, token, userID, displayName string) (domain.Participant, error) {
	displayName = strings.TrimSpace(displayName)
	if err := validateDisplayName(displayName); err != nil {
		return domain.Participant{}, err
	}
	return s.store.JoinEvent(ctx, hashToken(token), userID, displayName)
}

func (s *Service) ListParticipants(ctx context.Context, eventID, userID string) ([]domain.Participant, error) {
	return s.store.ListParticipants(ctx, eventID, userID)
}

func (s *Service) LeaveEvent(ctx context.Context, eventID, userID string) error {
	return s.store.LeaveEvent(ctx, eventID, userID)
}

type IntervalInput struct{ StartAt, EndAt time.Time }

func (s *Service) ReplaceAvailability(ctx context.Context, eventID, userID string, inputs []IntervalInput) ([]domain.AvailabilityInterval, error) {
	if len(inputs) > 100 {
		return nil, &ValidationError{Fields: map[string]string{"intervals": "100件以下で指定してください"}}
	}
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return nil, err
	}
	if event.Status != "open" {
		return nil, domain.ErrEventNotOpen
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].StartAt.Before(inputs[j].StartAt) })
	merged := make([]domain.AvailabilityInterval, 0, len(inputs))
	for _, input := range inputs {
		if input.StartAt.IsZero() || input.EndAt.IsZero() || !input.StartAt.Before(input.EndAt) {
			return nil, &ValidationError{Fields: map[string]string{"intervals": "開始日時より後の終了日時を指定してください"}}
		}
		input.StartAt, input.EndAt = input.StartAt.UTC(), input.EndAt.UTC()
		if input.StartAt.Before(event.WindowStartAt) || input.EndAt.After(event.WindowEndAt) {
			return nil, &ValidationError{Fields: map[string]string{"intervals": "イベント期間内を指定してください"}}
		}
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if input.StartAt.Before(last.EndAt) {
				return nil, &ValidationError{Fields: map[string]string{"intervals": "時間帯が重複しています"}}
			}
			if input.StartAt.Equal(last.EndAt) {
				last.EndAt = input.EndAt
				continue
			}
		}
		merged = append(merged, domain.AvailabilityInterval{StartAt: input.StartAt, EndAt: input.EndAt, Source: "manual"})
	}
	return s.store.ReplaceAvailability(ctx, eventID, userID, merged)
}

func (s *Service) GetOwnAvailability(ctx context.Context, eventID, userID string) ([]domain.AvailabilityInterval, string, error) {
	return s.store.GetOwnAvailability(ctx, eventID, userID)
}

func (s *Service) CopyOwnAvailability(ctx context.Context, targetEventID, sourceEventID, userID string) ([]domain.AvailabilityInterval, error) {
	sourceEventID = strings.TrimSpace(sourceEventID)
	if sourceEventID == "" {
		return nil, &ValidationError{Fields: map[string]string{"source_event_id": "コピー元イベントを指定してください"}}
	}
	if sourceEventID == targetEventID {
		return nil, &ValidationError{Fields: map[string]string{"source_event_id": "コピー先とは別のイベントを指定してください"}}
	}
	isGoogleUser, err := s.store.HasGoogleAccount(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !isGoogleUser {
		return nil, domain.ErrGoogleAuthRequired
	}

	sourceIntervals, sourceStatus, err := s.store.GetOwnAvailability(ctx, sourceEventID, userID)
	if err != nil {
		return nil, err
	}
	if sourceStatus != "submitted" {
		return nil, &ValidationError{Fields: map[string]string{"source_event_id": "コピー元イベントで回答を完了してください"}}
	}
	targetEvent, err := s.store.GetEventForUser(ctx, targetEventID, userID)
	if err != nil {
		return nil, err
	}
	if targetEvent.Status != "open" {
		return nil, domain.ErrEventNotOpen
	}
	targetIntervals, _, err := s.store.GetOwnAvailability(ctx, targetEventID, userID)
	if err != nil {
		return nil, err
	}

	inputs := make([]IntervalInput, 0, len(targetIntervals)+len(sourceIntervals))
	for _, interval := range targetIntervals {
		inputs = append(inputs, IntervalInput{StartAt: interval.StartAt, EndAt: interval.EndAt})
	}
	copiedCount := 0
	for _, interval := range sourceIntervals {
		startAt, endAt := interval.StartAt, interval.EndAt
		if startAt.Before(targetEvent.WindowStartAt) {
			startAt = targetEvent.WindowStartAt
		}
		if endAt.After(targetEvent.WindowEndAt) {
			endAt = targetEvent.WindowEndAt
		}
		if startAt.Before(endAt) {
			inputs = append(inputs, IntervalInput{StartAt: startAt, EndAt: endAt})
			copiedCount++
		}
	}
	if len(sourceIntervals) > 0 && copiedCount == 0 {
		return nil, &ValidationError{Fields: map[string]string{"source_event_id": "現在イベントの期間と重なる回答がありません"}}
	}

	sort.Slice(inputs, func(i, j int) bool { return inputs[i].StartAt.Before(inputs[j].StartAt) })
	merged := make([]IntervalInput, 0, len(inputs))
	for _, input := range inputs {
		if len(merged) == 0 || input.StartAt.After(merged[len(merged)-1].EndAt) {
			merged = append(merged, input)
			continue
		}
		if input.EndAt.After(merged[len(merged)-1].EndAt) {
			merged[len(merged)-1].EndAt = input.EndAt
		}
	}
	return s.ReplaceAvailability(ctx, targetEventID, userID, merged)
}

func (s *Service) ListAvailability(ctx context.Context, eventID, userID string) ([]domain.ParticipantAvailability, error) {
	return s.store.ListAvailability(ctx, eventID, userID)
}

func (s *Service) Candidates(ctx context.Context, eventID, userID string, limit int) ([]domain.Candidate, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return nil, err
	}
	availability, err := s.store.ListAvailability(ctx, eventID, userID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return calculateCandidates(event, availability, limit), nil
}

func (s *Service) ConfirmEvent(ctx context.Context, eventID, userID string, startAt, endAt time.Time) (domain.Event, error) {
	event, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return domain.Event{}, err
	}
	if event.CreatedByUserID != userID {
		return domain.Event{}, domain.ErrPermissionDenied
	}
	startAt, endAt = startAt.UTC(), endAt.UTC()
	fields := map[string]string{}
	if !startAt.Before(endAt) {
		fields["end_at"] = "start_atより後を指定してください"
	}
	if startAt.Before(event.WindowStartAt) || endAt.After(event.WindowEndAt) {
		fields["start_at"] = "イベント期間内を指定してください"
	}
	if endAt.Sub(startAt) != time.Duration(event.DurationMinutes)*time.Minute {
		fields["end_at"] = "イベントの必要時間と一致させてください"
	}
	if len(fields) > 0 {
		return domain.Event{}, &ValidationError{Fields: fields}
	}
	confirmed, err := s.store.ConfirmEvent(ctx, eventID, userID, startAt, endAt)
	if err != nil {
		return domain.Event{}, err
	}
	if s.GoogleEnabled() {
		participants, listErr := s.store.ListParticipants(ctx, eventID, userID)
		if listErr != nil {
			logCalendarSyncFailure(ctx, "list event participants", eventID, "", listErr)
		} else {
			s.syncConfirmationCalendars(ctx, participants, eventCalendarEntry(s.publicAppURL, event, nil, startAt, endAt), eventID, "")
		}
	}
	return confirmed, nil
}

func (s *Service) ReopenEvent(ctx context.Context, eventID, userID string) (domain.Event, error) {
	_, err := s.store.GetEventForUser(ctx, eventID, userID)
	if err != nil {
		return domain.Event{}, err
	}
	reopened, err := s.store.ReopenEvent(ctx, eventID, userID)
	if err != nil {
		return domain.Event{}, err
	}
	if s.GoogleEnabled() {
		participants, listErr := s.store.ListParticipants(ctx, eventID, userID)
		if listErr != nil {
			logCalendarSyncFailure(ctx, "list event participants", eventID, "", listErr)
		} else {
			s.deleteConfirmationCalendars(ctx, participants, confirmationCalendarEventID(eventID, ""), eventID, "")
		}
	}
	return reopened, nil
}

func (s *Service) CancelEvent(ctx context.Context, eventID, userID string) error {
	return s.store.CancelEvent(ctx, eventID, userID)
}

func normalizeEvent(input CreateEventInput) CreateEventInput {
	input.Title = strings.TrimSpace(input.Title)
	input.TimeZone = strings.TrimSpace(input.TimeZone)
	input.SchedulingMode = strings.TrimSpace(input.SchedulingMode)
	if input.SchedulingMode == "" {
		input.SchedulingMode = "whole_event"
	}
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if value == "" {
			input.Description = nil
		} else {
			input.Description = &value
		}
	}
	input.WindowStartAt = input.WindowStartAt.UTC()
	input.WindowEndAt = input.WindowEndAt.UTC()
	if input.ResponseDeadlineAt != nil {
		value := input.ResponseDeadlineAt.UTC()
		input.ResponseDeadlineAt = &value
	}
	return input
}

func validateEvent(input CreateEventInput) error {
	fields := map[string]string{}
	if utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 200 {
		fields["title"] = "1～200文字で指定してください"
	}
	if input.Description != nil && utf8.RuneCountInString(*input.Description) > 5000 {
		fields["description"] = "5000文字以下で指定してください"
	}
	if _, err := time.LoadLocation(input.TimeZone); err != nil {
		fields["time_zone"] = "有効なIANAタイムゾーンを指定してください"
	}
	if input.WindowStartAt.IsZero() || input.WindowEndAt.IsZero() || !input.WindowStartAt.Before(input.WindowEndAt) {
		fields["window_end_at"] = "window_start_atより後を指定してください"
	}
	if input.DurationMinutes < 5 || input.DurationMinutes > 1440 {
		fields["duration_minutes"] = "5～1440分で指定してください"
	}
	if input.SchedulingMode != "whole_event" && input.SchedulingMode != "groups" {
		fields["scheduling_mode"] = "whole_event、groupsのいずれかを指定してください"
	}
	if input.WindowEndAt.Sub(input.WindowStartAt) < time.Duration(input.DurationMinutes)*time.Minute {
		fields["duration_minutes"] = "イベント期間以下にしてください"
	}
	if input.ResponseDeadlineAt != nil && input.ResponseDeadlineAt.After(input.WindowEndAt) {
		fields["response_deadline_at"] = "window_end_at以前を指定してください"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func validateDisplayName(name string) error {
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return &ValidationError{Fields: map[string]string{"display_name": "1～100文字で指定してください"}}
	}
	return nil
}

func newToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func calculateCandidates(event domain.Event, availability []domain.ParticipantAvailability, limit int) []domain.Candidate {
	duration := time.Duration(event.DurationMinutes) * time.Minute
	starts := map[int64]time.Time{event.WindowStartAt.UnixNano(): event.WindowStartAt}
	for _, participant := range availability {
		for _, interval := range participant.Intervals {
			starts[interval.StartAt.UnixNano()] = interval.StartAt
			latest := interval.EndAt.Add(-duration)
			if !latest.Before(interval.StartAt) {
				starts[latest.UnixNano()] = latest
			}
		}
	}
	candidates := make([]domain.Candidate, 0)
	for _, start := range starts {
		end := start.Add(duration)
		if start.Before(event.WindowStartAt) || end.After(event.WindowEndAt) {
			continue
		}
		ids := make([]string, 0)
		for _, participant := range availability {
			for _, interval := range participant.Intervals {
				if !start.Before(interval.StartAt) && !end.After(interval.EndAt) {
					ids = append(ids, participant.Participant.ID)
					break
				}
			}
		}
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		candidates = append(candidates, domain.Candidate{StartAt: start, EndAt: end, AvailableParticipantCount: len(ids), TotalParticipantCount: len(availability), AvailableParticipantIDs: ids})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].AvailableParticipantCount != candidates[j].AvailableParticipantCount {
			return candidates[i].AvailableParticipantCount > candidates[j].AvailableParticipantCount
		}
		return candidates[i].StartAt.Before(candidates[j].StartAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}
