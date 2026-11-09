package repository

import (
	"context"

	"github.com/astalum/vacaro-back/internal/domain"
)

func (s *Store) UpsertICloudCalendarSync(ctx context.Context, eventID string, groupID *string, userID, calendarID, status string) (domain.ConfirmationCalendarSync, error) {
	query := `
		INSERT INTO icloud_calendar_syncs (event_id, group_id, user_id, calendar_id, status)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		ON CONFLICT (event_id, group_id, user_id) WHERE group_id IS NOT NULL
		DO UPDATE SET status = EXCLUDED.status, calendar_id = EXCLUDED.calendar_id, attempted_at = CURRENT_TIMESTAMP
		RETURNING event_id::text, group_id::text, user_id::text, status, attempted_at
	`
	if groupID == nil {
		query = `
			INSERT INTO icloud_calendar_syncs (event_id, group_id, user_id, calendar_id, status)
			VALUES ($1, NULL, $2, NULLIF($3, ''), $4)
			ON CONFLICT (event_id, user_id) WHERE group_id IS NULL
			DO UPDATE SET status = EXCLUDED.status, calendar_id = EXCLUDED.calendar_id, attempted_at = CURRENT_TIMESTAMP
			RETURNING event_id::text, group_id::text, user_id::text, status, attempted_at
		`
		return scanConfirmationCalendarSync(s.pool.QueryRow(ctx, query, eventID, userID, calendarID, status))
	}
	return scanConfirmationCalendarSync(s.pool.QueryRow(ctx, query, eventID, *groupID, userID, calendarID, status))
}

func (s *Store) ListICloudCalendarSyncs(ctx context.Context, eventID, userID string) ([]domain.ConfirmationCalendarSync, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT event_id::text, group_id::text, user_id::text, status, attempted_at
		FROM icloud_calendar_syncs
		WHERE event_id = $1 AND user_id = $2
			AND (status = 'failed' OR calendar_id = (SELECT calendar_id FROM icloud_calendar_connections WHERE user_id = $2))
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

func (s *Store) ClearICloudCalendarSync(ctx context.Context, eventID string, groupID *string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM icloud_calendar_syncs
		WHERE event_id = $1
			AND (($2::uuid IS NULL AND group_id IS NULL) OR group_id = $2::uuid)
	`, eventID, groupID)
	return err
}
