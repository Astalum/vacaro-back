package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) GetCalendarProvider(ctx context.Context, id string) (string, error) {
	var provider string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT provider FROM user_calendar_preferences WHERE user_id=$1),'none')`, id).Scan(&provider)
	return provider, err
}

// A bounded, dedicated connection holds the cross-process lock. Repository
// calls inside fn can use the main pool even when other users are syncing.
func (s *Store) WithCalendarLock(ctx context.Context, id string, fn func() error) error {
	select {
	case s.calendarLocks <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.calendarLocks }()
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(cleanup)
	}()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('vacaro:calendar-provider:' || $1,0))`, id); err != nil {
		return err
	}
	// Closing the dedicated connection releases its session advisory lock.
	return fn()
}
func (s *Store) SetCalendarProvider(ctx context.Context, id, provider string) error {
	return s.WithCalendarLock(ctx, id, func() error {
		_, err := s.pool.Exec(ctx, `INSERT INTO user_calendar_preferences(user_id,provider) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET provider=EXCLUDED.provider,updated_at=CURRENT_TIMESTAMP`, id, provider)
		return err
	})
}
