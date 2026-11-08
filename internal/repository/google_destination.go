package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Destination IDs survive disconnects so reconnecting reuses the same calendar.
func (s *Store) GetGoogleCalendarDestination(ctx context.Context, userID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT calendar_id FROM google_calendar_destinations WHERE user_id = $1`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) ResolveGoogleCalendarDestination(ctx context.Context, userID string, resolve func(string) (string, error)) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Lock a row that exists before the destination does; locking the destination
	// alone would not serialize concurrent first-time creation.
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id = $1 AND anonymized_at IS NULL FOR UPDATE`, userID).Scan(&locked); err != nil {
		return "", translateNotFound(err)
	}
	var current string
	err = tx.QueryRow(ctx, `SELECT calendar_id FROM google_calendar_destinations WHERE user_id = $1`, userID).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	id, err := resolve(current)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(id) == "" || strings.EqualFold(strings.TrimSpace(id), "primary") {
		return "", domain.ErrCalendarSyncFailed
	}
	if id != current {
		if _, err := tx.Exec(ctx, `INSERT INTO google_calendar_destinations (user_id, calendar_id) VALUES ($1, $2)
   ON CONFLICT (user_id) DO UPDATE SET calendar_id = EXCLUDED.calendar_id, updated_at = CURRENT_TIMESTAMP`, userID, id); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}
