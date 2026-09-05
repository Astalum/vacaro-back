package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/astalum/vacaro-back/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) UpsertGoogleUser(ctx context.Context, currentUserID, subject, email, displayName string) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var targetUserID string
	err = tx.QueryRow(ctx, `
		SELECT user_id::text FROM auth_accounts WHERE provider = 'google' AND provider_subject = $1 FOR UPDATE
	`, subject).Scan(&targetUserID)
	switch {
	case err == nil:
		if currentUserID != "" && currentUserID != targetUserID {
			if err := mergeGuestUser(ctx, tx, currentUserID, targetUserID); err != nil {
				return domain.User{}, err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE users SET account_type = 'registered', display_name = $2, email = $3,
				guest_expires_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1
		`, targetUserID, displayName, email); err != nil {
			return domain.User{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE auth_accounts SET email = $2, last_login_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			WHERE provider = 'google' AND provider_subject = $1
		`, subject, email); err != nil {
			return domain.User{}, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		if currentUserID != "" {
			var accountType string
			if err := tx.QueryRow(ctx, `SELECT account_type FROM users WHERE id = $1 FOR UPDATE`, currentUserID).Scan(&accountType); err != nil {
				return domain.User{}, translateNotFound(err)
			}
			targetUserID = currentUserID
			if _, err := tx.Exec(ctx, `
				UPDATE users SET account_type = 'registered', display_name = $2, email = $3,
					guest_expires_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1
			`, targetUserID, displayName, email); err != nil {
				return domain.User{}, err
			}
		} else {
			if err := tx.QueryRow(ctx, `
				INSERT INTO users (account_type, display_name, email)
				VALUES ('registered', $1, $2) RETURNING id::text
			`, displayName, email).Scan(&targetUserID); err != nil {
				return domain.User{}, err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO auth_accounts (user_id, provider, provider_subject, email, last_login_at)
			VALUES ($1, 'google', $2, $3, CURRENT_TIMESTAMP)
		`, targetUserID, subject, email); err != nil {
			return domain.User{}, err
		}
	default:
		return domain.User{}, err
	}

	user, err := scanUser(tx.QueryRow(ctx, `
		SELECT id::text, account_type, display_name, email, guest_expires_at, created_at, updated_at
		FROM users WHERE id = $1
	`, targetUserID))
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func mergeGuestUser(ctx context.Context, tx pgx.Tx, guestUserID, targetUserID string) error {
	var accountType string
	if err := tx.QueryRow(ctx, `SELECT account_type FROM users WHERE id = $1 FOR UPDATE`, guestUserID).Scan(&accountType); err != nil {
		return translateNotFound(err)
	}
	if accountType != "guest" {
		return domain.ErrPermissionDenied
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM participants target_participant
		USING events guest_event
		WHERE target_participant.event_id = guest_event.id
			AND guest_event.created_by_user_id = $1
			AND target_participant.user_id = $2
	`, guestUserID, targetUserID); err != nil {
		return fmt.Errorf("remove duplicate participants from guest events: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM participants guest_participant
		USING participants target_participant
		WHERE guest_participant.user_id = $1
			AND target_participant.user_id = $2
			AND guest_participant.event_id = target_participant.event_id
	`, guestUserID, targetUserID); err != nil {
		return fmt.Errorf("remove duplicate guest participations: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE events SET created_by_user_id = $2 WHERE created_by_user_id = $1`, guestUserID, targetUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE participants SET user_id = $2 WHERE user_id = $1`, guestUserID, targetUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, guestUserID); err != nil {
		return err
	}
	return nil
}

func (s *Store) SaveGoogleCalendarConnection(ctx context.Context, userID string, ciphertext []byte, scopes []string) error {
	command, err := s.pool.Exec(ctx, `
		INSERT INTO google_calendar_connections (auth_account_id, refresh_token_ciphertext, scopes)
		SELECT id, $2, $3 FROM auth_accounts WHERE user_id = $1 AND provider = 'google'
		ON CONFLICT (auth_account_id) DO UPDATE
		SET refresh_token_ciphertext = EXCLUDED.refresh_token_ciphertext,
			scopes = EXCLUDED.scopes, connected_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
	`, userID, ciphertext, scopes)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return domain.ErrPermissionDenied
	}
	return nil
}

func (s *Store) GetGoogleCalendarConnection(ctx context.Context, userID string) ([]byte, []string, error) {
	var ciphertext []byte
	var scopes []string
	err := s.pool.QueryRow(ctx, `
		SELECT gcc.refresh_token_ciphertext, gcc.scopes
		FROM google_calendar_connections gcc
		JOIN auth_accounts aa ON aa.id = gcc.auth_account_id
		WHERE aa.user_id = $1 AND aa.provider = 'google'
	`, userID).Scan(&ciphertext, &scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, domain.ErrCalendarNotConnected
	}
	return ciphertext, scopes, err
}

func (s *Store) DeleteGoogleCalendarConnection(ctx context.Context, userID string) ([]byte, error) {
	var ciphertext []byte
	err := s.pool.QueryRow(ctx, `
		DELETE FROM google_calendar_connections gcc
		USING auth_accounts aa
		WHERE gcc.auth_account_id = aa.id AND aa.user_id = $1 AND aa.provider = 'google'
		RETURNING gcc.refresh_token_ciphertext
	`, userID).Scan(&ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCalendarNotConnected
	}
	return ciphertext, err
}

func (s *Store) HasGoogleAccount(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM auth_accounts aa
			JOIN users u ON u.id = aa.user_id
			WHERE aa.user_id = $1 AND aa.provider = 'google'
				AND u.account_type = 'registered' AND u.anonymized_at IS NULL
		)
	`, userID).Scan(&exists)
	return exists, err
}
