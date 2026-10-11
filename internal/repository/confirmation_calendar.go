package repository

import (
	"context"
	"time"

	"github.com/astalum/vacaro-back/internal/domain"
)

func (s *Store) UpsertConfirmationCalendarSync(ctx context.Context, eventID string, groupID *string, userID, status string) (domain.ConfirmationCalendarSync, error) {
	query := `
		INSERT INTO confirmation_calendar_syncs (event_id, group_id, user_id, status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (event_id, group_id, user_id) WHERE group_id IS NOT NULL
		DO UPDATE SET status = EXCLUDED.status, attempted_at = CURRENT_TIMESTAMP
		RETURNING event_id::text, group_id::text, user_id::text, status, attempted_at
	`
	if groupID == nil {
		query = `
			INSERT INTO confirmation_calendar_syncs (event_id, group_id, user_id, status)
			VALUES ($1, NULL, $2, $3)
			ON CONFLICT (event_id, user_id) WHERE group_id IS NULL
			DO UPDATE SET status = EXCLUDED.status, attempted_at = CURRENT_TIMESTAMP
			RETURNING event_id::text, group_id::text, user_id::text, status, attempted_at
		`
		return scanConfirmationCalendarSync(s.pool.QueryRow(ctx, query, eventID, userID, status))
	}
	return scanConfirmationCalendarSync(s.pool.QueryRow(ctx, query, eventID, *groupID, userID, status))
}

func (s *Store) ListConfirmationCalendarSyncs(ctx context.Context, eventID, userID string) ([]domain.ConfirmationCalendarSync, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT event_id::text, group_id::text, user_id::text, status, attempted_at
		FROM confirmation_calendar_syncs
		WHERE event_id = $1 AND user_id = $2
		ORDER BY group_id NULLS FIRST
	`, eventID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.ConfirmationCalendarSync, 0)
	for rows.Next() {
		sync, err := scanConfirmationCalendarSync(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, sync)
	}
	return result, rows.Err()
}

func (s *Store) ClearConfirmationCalendarSync(ctx context.Context, eventID string, groupID *string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM confirmation_calendar_syncs
		WHERE event_id = $1
			AND (($2::uuid IS NULL AND group_id IS NULL) OR group_id = $2::uuid)
	`, eventID, groupID)
	return err
}

type confirmationCalendarSyncScanner interface {
	Scan(...any) error
}

func scanConfirmationCalendarSync(row confirmationCalendarSyncScanner) (domain.ConfirmationCalendarSync, error) {
	var result domain.ConfirmationCalendarSync
	var attemptedAt time.Time
	err := row.Scan(&result.EventID, &result.GroupID, &result.UserID, &result.Status, &attemptedAt)
	result.AttemptedAt = &attemptedAt
	return result, err
}
