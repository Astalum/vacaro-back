package repository

import (
	"context"
	"github.com/astalum/vacaro-back/internal/domain"
)

func (s *Store) SetAvailabilityDefault(ctx context.Context, userID, value string) (domain.User, error) {
	row := s.pool.QueryRow(ctx, `
        UPDATE users
        SET availability_default = $2, updated_at = CURRENT_TIMESTAMP
        WHERE id = $1 AND anonymized_at IS NULL
        RETURNING id::text, account_type, display_name, time_zone, availability_default, email, guest_expires_at, created_at, updated_at
    `, userID, value)
	return scanUser(row)
}
