package repository

import (
	"context"
	"errors"
	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) SaveICloudConnection(ctx context.Context, userID string, ciphertext []byte, calendarID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO icloud_calendar_connections(user_id,credentials_ciphertext,calendar_id) VALUES($1,$2,$3)
 ON CONFLICT(user_id) DO UPDATE SET credentials_ciphertext=EXCLUDED.credentials_ciphertext,calendar_id=EXCLUDED.calendar_id,connected_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP`, userID, ciphertext, calendarID)
	return err
}
func (s *Store) GetICloudConnection(ctx context.Context, userID string) ([]byte, string, error) {
	var ciphertext []byte
	var calendarID string
	err := s.pool.QueryRow(ctx, `SELECT credentials_ciphertext,calendar_id FROM icloud_calendar_connections WHERE user_id=$1`, userID).Scan(&ciphertext, &calendarID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", domain.ErrICloudNotConnected
	}
	return ciphertext, calendarID, err
}
func (s *Store) DeleteICloudConnection(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM icloud_calendar_connections WHERE user_id=$1`, userID)
	return err
}

func (s *Store) ClearAllICloudCalendarSyncs(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM icloud_calendar_syncs WHERE user_id=$1`, userID)
	return err
}
