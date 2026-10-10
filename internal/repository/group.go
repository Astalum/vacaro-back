package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type CreateGroupParams struct {
	EventID         string
	UserID          string
	Name            string
	DurationMinutes int
	ParticipantIDs  []string
}

type UpdateGroupParams struct {
	EventID         string
	GroupID         string
	UserID          string
	Name            string
	DurationMinutes int
}

func (s *Store) ListGroups(ctx context.Context, eventID, userID string) ([]domain.SchedulingGroup, error) {
	if err := s.requireParticipant(ctx, eventID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+groupColumns+`
		FROM scheduling_groups g
		WHERE g.event_id = $1
		ORDER BY g.created_at, g.id
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]domain.SchedulingGroup, 0)
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range groups {
		members, err := s.listGroupMembers(ctx, groups[i].ID)
		if err != nil {
			return nil, err
		}
		groups[i].Members = members
		groups[i].MemberCount = len(members)
	}
	return groups, nil
}

func (s *Store) GetGroup(ctx context.Context, eventID, groupID, userID string) (domain.SchedulingGroup, error) {
	if err := s.requireParticipant(ctx, eventID, userID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	group, err := scanGroup(s.pool.QueryRow(ctx, `
		SELECT `+groupColumns+`
		FROM scheduling_groups g
		WHERE g.event_id = $1 AND g.id = $2
	`, eventID, groupID))
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	members, err := s.listGroupMembers(ctx, group.ID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	group.Members = members
	group.MemberCount = len(members)
	return group, nil
}

func (s *Store) CreateGroup(ctx context.Context, params CreateGroupParams) (domain.SchedulingGroup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requireEditableGroupEvent(ctx, tx, params.EventID, params.UserID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	group, err := scanGroup(tx.QueryRow(ctx, `
		INSERT INTO scheduling_groups (event_id, name, duration_minutes)
		VALUES ($1, $2, $3)
		RETURNING `+groupColumnsNoPrefix,
		params.EventID, params.Name, params.DurationMinutes))
	if err != nil {
		return domain.SchedulingGroup{}, translateGroupWriteError(err)
	}
	if err := replaceMembersTx(ctx, tx, params.EventID, group.ID, params.ParticipantIDs); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SchedulingGroup{}, err
	}
	return s.GetGroup(ctx, params.EventID, group.ID, params.UserID)
}

func (s *Store) UpdateGroup(ctx context.Context, params UpdateGroupParams) (domain.SchedulingGroup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requireEditableGroupEvent(ctx, tx, params.EventID, params.UserID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	var confirmedStart *time.Time
	var currentDuration int
	if err := tx.QueryRow(ctx, `
		SELECT confirmed_start_at, duration_minutes
		FROM scheduling_groups
		WHERE id = $1 AND event_id = $2
		FOR UPDATE
	`, params.GroupID, params.EventID).Scan(&confirmedStart, &currentDuration); err != nil {
		return domain.SchedulingGroup{}, translateNotFound(err)
	}
	if confirmedStart != nil && params.DurationMinutes != currentDuration {
		return domain.SchedulingGroup{}, domain.ErrGroupConfirmationExists
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scheduling_groups
		SET name = $3, duration_minutes = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND event_id = $2
	`, params.GroupID, params.EventID, params.Name, params.DurationMinutes); err != nil {
		return domain.SchedulingGroup{}, translateGroupWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SchedulingGroup{}, err
	}
	return s.GetGroup(ctx, params.EventID, params.GroupID, params.UserID)
}

func (s *Store) DeleteGroup(ctx context.Context, eventID, groupID, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requireEditableGroupEvent(ctx, tx, eventID, userID); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		DELETE FROM scheduling_groups
		WHERE id = $1 AND event_id = $2 AND confirmed_start_at IS NULL
	`, groupID, eventID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM scheduling_groups WHERE id = $1 AND event_id = $2
			)
		`, groupID, eventID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return domain.ErrGroupConfirmationExists
		}
		return domain.ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *Store) ReplaceGroupMembers(ctx context.Context, eventID, groupID, userID string, participantIDs []string) (domain.SchedulingGroup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requireEditableGroupEvent(ctx, tx, eventID, userID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	var confirmedStart *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT confirmed_start_at FROM scheduling_groups
		WHERE id = $1 AND event_id = $2 FOR UPDATE
	`, groupID, eventID).Scan(&confirmedStart); err != nil {
		return domain.SchedulingGroup{}, translateNotFound(err)
	}
	if confirmedStart != nil {
		return domain.SchedulingGroup{}, domain.ErrGroupConfirmationExists
	}
	if err := replaceMembersTx(ctx, tx, eventID, groupID, participantIDs); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scheduling_groups SET updated_at = CURRENT_TIMESTAMP WHERE id = $1
	`, groupID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SchedulingGroup{}, err
	}
	return s.GetGroup(ctx, eventID, groupID, userID)
}

func (s *Store) ListGroupAvailability(ctx context.Context, eventID, groupID, userID string) ([]domain.ParticipantAvailability, error) {
	if _, err := s.GetGroup(ctx, eventID, groupID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+prefixedParticipantColumns("p")+`
		FROM participants p
		JOIN scheduling_group_members gm ON gm.participant_id = p.id
		WHERE gm.group_id = $1 AND p.event_id = $2
		ORDER BY p.role DESC, p.created_at, p.id
	`, groupID, eventID)
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
	if err := rows.Err(); err != nil {
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

func (s *Store) ConfirmGroup(ctx context.Context, eventID, groupID, userID string, startAt, endAt time.Time) (domain.SchedulingGroup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requireConfirmableGroupEvent(ctx, tx, eventID, userID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	var memberCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM scheduling_group_members gm
		JOIN scheduling_groups g ON g.id = gm.group_id
		WHERE g.id = $1 AND g.event_id = $2
	`, groupID, eventID).Scan(&memberCount); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if memberCount == 0 {
		return domain.SchedulingGroup{}, domain.ErrGroupNotEditable
	}
	command, err := tx.Exec(ctx, `
		UPDATE scheduling_groups
		SET confirmed_start_at = $3, confirmed_end_at = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND event_id = $2
	`, groupID, eventID, startAt, endAt)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	if command.RowsAffected() == 0 {
		return domain.SchedulingGroup{}, domain.ErrNotFound
	}
	var groupCount, confirmedCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int, COUNT(confirmed_start_at)::int
		FROM scheduling_groups WHERE event_id = $1
	`, eventID).Scan(&groupCount, &confirmedCount); err != nil {
		return domain.SchedulingGroup{}, err
	}
	status := "open"
	if groupCount > 0 && groupCount == confirmedCount {
		status = "confirmed"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE events
		SET status = CASE
				WHEN confirmed_start_at IS NOT NULL THEN 'confirmed'
				ELSE $2
			END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, eventID, status); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if err := s.blockConfirmedTimeInOtherEvents(ctx, tx, eventID, &groupID, startAt, endAt); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SchedulingGroup{}, err
	}
	return s.GetGroup(ctx, eventID, groupID, userID)
}

func (s *Store) ReopenGroup(ctx context.Context, eventID, groupID, userID string) (domain.SchedulingGroup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ownerID, mode, status string
	if err := tx.QueryRow(ctx, `
		SELECT created_by_user_id::text, scheduling_mode, status
		FROM events WHERE id = $1 FOR UPDATE
	`, eventID).Scan(&ownerID, &mode, &status); err != nil {
		return domain.SchedulingGroup{}, translateNotFound(err)
	}
	if ownerID != userID {
		return domain.SchedulingGroup{}, domain.ErrPermissionDenied
	}
	if mode != "groups" || status == "cancelled" {
		return domain.SchedulingGroup{}, domain.ErrGroupNotEditable
	}
	command, err := tx.Exec(ctx, `
		UPDATE scheduling_groups
		SET confirmed_start_at = NULL, confirmed_end_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND event_id = $2
	`, groupID, eventID)
	if err != nil {
		return domain.SchedulingGroup{}, err
	}
	if command.RowsAffected() == 0 {
		return domain.SchedulingGroup{}, domain.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE events
		SET status = CASE
				WHEN confirmed_start_at IS NULL THEN 'open'
				ELSE 'confirmed'
			END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, eventID); err != nil {
		return domain.SchedulingGroup{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.SchedulingGroup{}, err
	}
	return s.GetGroup(ctx, eventID, groupID, userID)
}

func requireEditableGroupEvent(ctx context.Context, tx pgx.Tx, eventID, userID string) error {
	var ownerID, status, mode string
	if err := tx.QueryRow(ctx, `
		SELECT created_by_user_id::text, status, scheduling_mode
		FROM events WHERE id = $1 FOR UPDATE
	`, eventID).Scan(&ownerID, &status, &mode); err != nil {
		return translateNotFound(err)
	}
	if ownerID != userID {
		return domain.ErrPermissionDenied
	}
	if status != "open" {
		return domain.ErrGroupNotEditable
	}
	if mode != "groups" {
		return domain.ErrGroupNotEditable
	}
	return nil
}

func requireConfirmableGroupEvent(ctx context.Context, tx pgx.Tx, eventID, userID string) error {
	var ownerID, status, mode string
	if err := tx.QueryRow(ctx, `
		SELECT created_by_user_id::text, status, scheduling_mode
		FROM events WHERE id = $1 FOR UPDATE
	`, eventID).Scan(&ownerID, &status, &mode); err != nil {
		return translateNotFound(err)
	}
	if ownerID != userID {
		return domain.ErrPermissionDenied
	}
	if status == "cancelled" || mode != "groups" {
		return domain.ErrGroupNotEditable
	}
	return nil
}

func replaceMembersTx(ctx context.Context, tx pgx.Tx, eventID, groupID string, participantIDs []string) error {
	if len(participantIDs) > 0 {
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)::int
			FROM participants
			WHERE event_id = $1 AND id = ANY($2::uuid[])
		`, eventID, participantIDs).Scan(&count); err != nil {
			return err
		}
		if count != len(participantIDs) {
			return domain.ErrNotFound
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM scheduling_group_members WHERE group_id = $1`, groupID); err != nil {
		return err
	}
	for _, participantID := range participantIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO scheduling_group_members (group_id, participant_id) VALUES ($1, $2)
		`, groupID, participantID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) listGroupMembers(ctx context.Context, groupID string) ([]domain.Participant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+prefixedParticipantColumns("p")+`
		FROM participants p
		JOIN scheduling_group_members gm ON gm.participant_id = p.id
		WHERE gm.group_id = $1
		ORDER BY p.role DESC, p.created_at, p.id
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]domain.Participant, 0)
	for rows.Next() {
		member, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

const groupColumns = `
	g.id::text, g.event_id::text, g.name, g.duration_minutes,
	g.confirmed_start_at, g.confirmed_end_at, g.created_at, g.updated_at`

const groupColumnsNoPrefix = `
	id::text, event_id::text, name, duration_minutes,
	confirmed_start_at, confirmed_end_at, created_at, updated_at`

func scanGroup(row rowScanner) (domain.SchedulingGroup, error) {
	var value domain.SchedulingGroup
	err := row.Scan(
		&value.ID, &value.EventID, &value.Name, &value.DurationMinutes,
		&value.ConfirmedStartAt, &value.ConfirmedEndAt, &value.CreatedAt, &value.UpdatedAt,
	)
	return value, translateNotFound(err)
}

func translateGroupWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.ErrDuplicateGroupName
	}
	return fmt.Errorf("write scheduling group: %w", err)
}
