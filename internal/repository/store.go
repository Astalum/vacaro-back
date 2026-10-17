package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) CreateGuest(ctx context.Context, displayName, timeZone string, expiresAt time.Time) (domain.User, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users (account_type, display_name, time_zone, guest_expires_at)
		VALUES ('guest', $1, $2, $3)
		RETURNING id::text, account_type, display_name, time_zone, email, guest_expires_at, created_at, updated_at
	`, displayName, timeZone, expiresAt)
	return scanUser(row)
}

func (s *Store) GetUser(ctx context.Context, userID string) (domain.User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, account_type, display_name, time_zone, email, guest_expires_at, created_at, updated_at
		FROM users
		WHERE id = $1 AND anonymized_at IS NULL
	`, userID)
	return scanUser(row)
}

func (s *Store) UpdateUser(ctx context.Context, userID, displayName, timeZone string) (domain.User, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE users
		SET display_name = $2, time_zone = $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND anonymized_at IS NULL
		RETURNING id::text, account_type, display_name, time_zone, email, guest_expires_at, created_at, updated_at
	`, userID, displayName, timeZone)
	return scanUser(row)
}

type CreateEventParams struct {
	CreatedByUserID              string
	InviteTokenHash              string
	Title                        string
	Description                  *string
	WindowStartAt                time.Time
	WindowEndAt                  time.Time
	DurationMinutes              int
	SchedulingMode               string
	ResponseDeadlineAt           *time.Time
	ResultsVisibleToParticipants *bool
	OrganizerName                string
}

func (s *Store) CreateEvent(ctx context.Context, params CreateEventParams) (domain.Event, error) {
	if params.SchedulingMode == "" {
		params.SchedulingMode = "whole_event"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Event{}, fmt.Errorf("begin create event: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		INSERT INTO events (
			created_by_user_id, invite_token_hash, title, description,
			window_start_at, window_end_at, duration_minutes, scheduling_mode, response_deadline_at,
			results_visible_to_participants
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10::boolean, TRUE))
		RETURNING `+eventColumns, params.CreatedByUserID, params.InviteTokenHash, params.Title,
		params.Description, params.WindowStartAt, params.WindowEndAt,
		params.DurationMinutes, params.SchedulingMode, params.ResponseDeadlineAt,
		params.ResultsVisibleToParticipants)
	event, err := scanEvent(row)
	if err != nil {
		return domain.Event{}, fmt.Errorf("insert event: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO participants (event_id, user_id, display_name, role)
		VALUES ($1, $2, $3, 'organizer')
	`, event.ID, params.CreatedByUserID, params.OrganizerName); err != nil {
		return domain.Event{}, fmt.Errorf("insert organizer: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Event{}, fmt.Errorf("commit create event: %w", err)
	}
	return event, nil
}

func (s *Store) ListEvents(ctx context.Context, userID, status string, cursorTime *time.Time, cursorID *string, limit int) ([]domain.Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+prefixedEventColumns("e")+`
		FROM events e
		JOIN participants p ON p.event_id = e.id
		WHERE p.user_id = $1
			AND ($2 = '' OR e.status = $2)
			AND (
				$3::timestamptz IS NULL
				OR e.created_at < $3
				OR (e.created_at = $3 AND e.id < $4::uuid)
			)
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $5
	`, userID, status, cursorTime, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	events := make([]domain.Event, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) GetEventForUser(ctx context.Context, eventID, userID string) (domain.Event, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+prefixedEventColumns("e")+`
		FROM events e
		JOIN participants p ON p.event_id = e.id
		WHERE e.id = $1 AND p.user_id = $2
	`, eventID, userID)
	return scanEvent(row)
}

func (s *Store) GetEventByInviteHash(ctx context.Context, tokenHash string) (domain.Event, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+eventColumns+`
		FROM events
		WHERE invite_token_hash = $1 AND status <> 'cancelled'
	`, tokenHash)
	return scanEvent(row)
}

type UpdateEventParams struct {
	EventID            string
	UserID             string
	Title              string
	Description        *string
	WindowStartAt      time.Time
	WindowEndAt        time.Time
	DurationMinutes    int
	SchedulingMode     string
	ResponseDeadlineAt *time.Time
}

func (s *Store) UpdateEvent(ctx context.Context, params UpdateEventParams) (domain.Event, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Event{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentMode string
	if err := tx.QueryRow(ctx, `
		SELECT e.scheduling_mode
		FROM events e
		WHERE e.id = $1 AND e.created_by_user_id = $2 AND e.status = 'open'
		FOR UPDATE OF e
	`, params.EventID, params.UserID).Scan(&currentMode); err != nil {
		return domain.Event{}, translateNotFound(err)
	}
	if params.SchedulingMode == "" {
		params.SchedulingMode = currentMode
	}
	if currentMode != params.SchedulingMode {
		var groupCount, confirmedCount int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)::int, COUNT(confirmed_start_at)::int
			FROM scheduling_groups WHERE event_id = $1
		`, params.EventID).Scan(&groupCount, &confirmedCount); err != nil {
			return domain.Event{}, err
		}
		if confirmedCount > 0 {
			return domain.Event{}, domain.ErrGroupConfirmationExists
		}
		if currentMode == "groups" && params.SchedulingMode == "whole_event" && groupCount > 0 {
			return domain.Event{}, domain.ErrGroupNotEditable
		}
	}
	row := tx.QueryRow(ctx, `
		UPDATE events
		SET title = $3, description = $4, window_start_at = $5,
			window_end_at = $6, duration_minutes = $7, scheduling_mode = $8,
			response_deadline_at = $9,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND created_by_user_id = $2 AND status = 'open'
		RETURNING `+eventColumns,
		params.EventID, params.UserID, params.Title, params.Description,
		params.WindowStartAt, params.WindowEndAt, params.DurationMinutes, params.SchedulingMode,
		params.ResponseDeadlineAt)
	event, err := scanEvent(row)
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Event{}, err
	}
	return event, nil
}

func (s *Store) SetResultsVisibility(ctx context.Context, eventID, userID string, visible bool) (domain.Event, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE events
		SET results_visible_to_participants = $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND created_by_user_id = $2
		RETURNING `+eventColumns,
		eventID, userID, visible)
	return scanEvent(row)
}

func (s *Store) RotateInviteToken(ctx context.Context, eventID, userID, tokenHash string) error {
	command, err := s.pool.Exec(ctx, `
		UPDATE events
		SET invite_token_hash = $3, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND created_by_user_id = $2 AND status = 'open'
	`, eventID, userID, tokenHash)
	if err != nil {
		return fmt.Errorf("rotate invite token: %w", err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) JoinEvent(ctx context.Context, tokenHash, userID, displayName string) (domain.Participant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Participant{}, fmt.Errorf("begin join event: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var eventID, status string
	var responseDeadlineAt *time.Time
	if err := tx.QueryRow(ctx, `
			SELECT id::text, status, response_deadline_at
			FROM events WHERE invite_token_hash = $1 FOR UPDATE
		`, tokenHash).Scan(&eventID, &status, &responseDeadlineAt); err != nil {
		return domain.Participant{}, translateNotFound(err)
	}
	if status != "open" {
		return domain.Participant{}, domain.ErrEventNotOpen
	}
	if responseDeadlineAt != nil && !time.Now().UTC().Before(*responseDeadlineAt) {
		return domain.Participant{}, domain.ErrResponseDeadlinePassed
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO participants (event_id, user_id, display_name)
		VALUES ($1, $2, $3)
		RETURNING `+participantColumns, eventID, userID, displayName)
	participant, err := scanParticipant(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.Participant{}, domain.ErrAlreadyJoined
		}
		return domain.Participant{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Participant{}, fmt.Errorf("commit join event: %w", err)
	}
	return participant, nil
}

func (s *Store) ListParticipants(ctx context.Context, eventID, userID string) ([]domain.Participant, error) {
	if err := s.requireParticipant(ctx, eventID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+participantColumns+`
		FROM participants
		WHERE event_id = $1
		ORDER BY role DESC, created_at, id
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	participants := make([]domain.Participant, 0)
	for rows.Next() {
		participant, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		participants = append(participants, participant)
	}
	return participants, rows.Err()
}

func (s *Store) LeaveEvent(ctx context.Context, eventID, userID string) error {
	command, err := s.pool.Exec(ctx, `
		DELETE FROM participants
		WHERE event_id = $1 AND user_id = $2 AND role = 'member'
	`, eventID, userID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		var role string
		err := s.pool.QueryRow(ctx, `SELECT role FROM participants WHERE event_id = $1 AND user_id = $2`, eventID, userID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if role == "organizer" {
			return domain.ErrOrganizerCannotLeave
		}
		return err
	}
	return nil
}

func (s *Store) ReplaceAvailability(ctx context.Context, eventID, userID string, intervals []domain.AvailabilityInterval) ([]domain.AvailabilityInterval, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var participantID, status string
	var windowStart, windowEnd time.Time
	var responseDeadlineAt *time.Time
	if err := tx.QueryRow(ctx, `
			SELECT p.id::text, e.status, e.window_start_at, e.window_end_at, e.response_deadline_at
		FROM participants p
		JOIN events e ON e.id = p.event_id
		WHERE p.event_id = $1 AND p.user_id = $2
		FOR UPDATE OF p, e
		`, eventID, userID).Scan(&participantID, &status, &windowStart, &windowEnd, &responseDeadlineAt); err != nil {
		return nil, translateNotFound(err)
	}
	if status != "open" {
		return nil, domain.ErrEventNotOpen
	}
	if responseDeadlineAt != nil && !time.Now().UTC().Before(*responseDeadlineAt) {
		return nil, domain.ErrResponseDeadlinePassed
	}
	for _, interval := range intervals {
		if interval.StartAt.Before(windowStart) || interval.EndAt.After(windowEnd) {
			return nil, fmt.Errorf("availability outside event window")
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM availability_intervals WHERE participant_id = $1`, participantID); err != nil {
		return nil, err
	}
	created := make([]domain.AvailabilityInterval, 0, len(intervals))
	for _, interval := range intervals {
		row := tx.QueryRow(ctx, `
			INSERT INTO availability_intervals (participant_id, start_at, end_at, source)
			VALUES ($1, $2, $3, $4)
			RETURNING `+availabilityColumns, participantID, interval.StartAt, interval.EndAt, interval.Source)
		item, err := scanAvailability(row)
		if err != nil {
			return nil, err
		}
		created = append(created, item)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE participants SET response_status = 'submitted', updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, participantID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) GetOwnAvailability(ctx context.Context, eventID, userID string) ([]domain.AvailabilityInterval, string, error) {
	var participantID, responseStatus string
	if err := s.pool.QueryRow(ctx, `
		SELECT id::text, response_status FROM participants WHERE event_id = $1 AND user_id = $2
	`, eventID, userID).Scan(&participantID, &responseStatus); err != nil {
		return nil, "", translateNotFound(err)
	}
	intervals, err := s.listIntervals(ctx, participantID)
	return intervals, responseStatus, err
}

func (s *Store) ListAvailability(ctx context.Context, eventID, userID string) ([]domain.ParticipantAvailability, error) {
	participants, err := s.ListParticipants(ctx, eventID, userID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.ParticipantAvailability, 0, len(participants))
	for _, participant := range participants {
		intervals, err := s.listIntervals(ctx, participant.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.ParticipantAvailability{Participant: participant, Intervals: intervals})
	}
	return result, nil
}

func (s *Store) ConfirmEvent(ctx context.Context, eventID, userID string, startAt, endAt time.Time) (domain.Event, error) {
	return s.changeEventState(ctx, eventID, userID, "confirmed", &startAt, &endAt, []string{"open", "confirmed"})
}

func (s *Store) ReopenEvent(ctx context.Context, eventID, userID string) (domain.Event, error) {
	return s.changeEventState(ctx, eventID, userID, "open", nil, nil, []string{"confirmed"})
}

func (s *Store) CancelEvent(ctx context.Context, eventID, userID string) error {
	event, err := s.changeEventState(ctx, eventID, userID, "cancelled", nil, nil, []string{"open", "confirmed", "cancelled"})
	_ = event
	return err
}

func (s *Store) changeEventState(ctx context.Context, eventID, userID, next string, startAt, endAt *time.Time, allowed []string) (domain.Event, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Event{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ownerID, status string
	if err := tx.QueryRow(ctx, `SELECT created_by_user_id::text, status FROM events WHERE id = $1 FOR UPDATE`, eventID).Scan(&ownerID, &status); err != nil {
		return domain.Event{}, translateNotFound(err)
	}
	if ownerID != userID {
		return domain.Event{}, domain.ErrPermissionDenied
	}
	allowedStatus := false
	for _, value := range allowed {
		allowedStatus = allowedStatus || status == value
	}
	if !allowedStatus {
		return domain.Event{}, domain.ErrInvalidTransition
	}

	row := tx.QueryRow(ctx, `
		UPDATE events e
		SET status = CASE
				WHEN $2 = 'open'
					AND e.scheduling_mode = 'groups'
					AND EXISTS (SELECT 1 FROM scheduling_groups g WHERE g.event_id = e.id)
					AND NOT EXISTS (
						SELECT 1 FROM scheduling_groups g
						WHERE g.event_id = e.id AND g.confirmed_start_at IS NULL
					)
				THEN 'confirmed'
				ELSE $2
			END,
			confirmed_start_at = $3,
			confirmed_end_at = $4,
			updated_at = CURRENT_TIMESTAMP
		WHERE e.id = $1
		RETURNING `+eventColumns, eventID, next, startAt, endAt)
	event, err := scanEvent(row)
	if err != nil {
		return domain.Event{}, err
	}
	if next == "confirmed" && startAt != nil && endAt != nil {
		if err := s.blockConfirmedTimeInOtherEvents(ctx, tx, eventID, nil, *startAt, *endAt); err != nil {
			return domain.Event{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Event{}, err
	}
	return event, nil
}

func (s *Store) requireParticipant(ctx context.Context, eventID, userID string) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM participants WHERE event_id = $1 AND user_id = $2)
	`, eventID, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) listIntervals(ctx context.Context, participantID string) ([]domain.AvailabilityInterval, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+availabilityColumns+`
		FROM availability_intervals
		WHERE participant_id = $1
		ORDER BY start_at, id
	`, participantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	intervals := make([]domain.AvailabilityInterval, 0)
	for rows.Next() {
		interval, err := scanAvailability(rows)
		if err != nil {
			return nil, err
		}
		intervals = append(intervals, interval)
	}
	return intervals, rows.Err()
}

const eventColumns = `
	id::text, created_by_user_id::text, title, description,
	window_start_at, window_end_at, duration_minutes, scheduling_mode, status,
	confirmed_start_at, confirmed_end_at, response_deadline_at,
	results_visible_to_participants, created_at, updated_at`

func prefixedEventColumns(prefix string) string {
	return prefix + `.id::text, ` + prefix + `.created_by_user_id::text, ` + prefix + `.title, ` +
		prefix + `.description, ` + prefix + `.window_start_at, ` +
		prefix + `.window_end_at, ` + prefix + `.duration_minutes, ` + prefix + `.scheduling_mode, ` + prefix + `.status, ` +
		prefix + `.confirmed_start_at, ` + prefix + `.confirmed_end_at, ` + prefix + `.response_deadline_at, ` +
		prefix + `.results_visible_to_participants, ` + prefix + `.created_at, ` + prefix + `.updated_at`
}

const participantColumns = `
	id::text, event_id::text, user_id::text, display_name, role, response_status, created_at, updated_at`

func prefixedParticipantColumns(prefix string) string {
	return prefix + `.id::text, ` + prefix + `.event_id::text, ` + prefix + `.user_id::text, ` +
		prefix + `.display_name, ` + prefix + `.role, ` + prefix + `.response_status, ` +
		prefix + `.created_at, ` + prefix + `.updated_at`
}

const availabilityColumns = `
	id::text, participant_id::text, start_at, end_at, source, created_at, updated_at`

type rowScanner interface {
	Scan(...any) error
}

func scanUser(row rowScanner) (domain.User, error) {
	var value domain.User
	err := row.Scan(&value.ID, &value.AccountType, &value.DisplayName, &value.TimeZone, &value.Email, &value.GuestExpiresAt, &value.CreatedAt, &value.UpdatedAt)
	return value, translateNotFound(err)
}

func scanEvent(row rowScanner) (domain.Event, error) {
	var value domain.Event
	err := row.Scan(
		&value.ID, &value.CreatedByUserID, &value.Title, &value.Description,
		&value.WindowStartAt, &value.WindowEndAt, &value.DurationMinutes, &value.SchedulingMode, &value.Status,
		&value.ConfirmedStartAt, &value.ConfirmedEndAt, &value.ResponseDeadlineAt,
		&value.ResultsVisibleToParticipants, &value.CreatedAt, &value.UpdatedAt,
	)
	return value, translateNotFound(err)
}

func scanParticipant(row rowScanner) (domain.Participant, error) {
	var value domain.Participant
	err := row.Scan(&value.ID, &value.EventID, &value.UserID, &value.DisplayName, &value.Role, &value.ResponseStatus, &value.CreatedAt, &value.UpdatedAt)
	return value, translateNotFound(err)
}

func scanAvailability(row rowScanner) (domain.AvailabilityInterval, error) {
	var value domain.AvailabilityInterval
	err := row.Scan(&value.ID, &value.ParticipantID, &value.StartAt, &value.EndAt, &value.Source, &value.CreatedAt, &value.UpdatedAt)
	return value, translateNotFound(err)
}

func translateNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}
