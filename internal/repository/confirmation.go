package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type availabilityRemainder struct {
	StartAt time.Time
	EndAt   time.Time
}

type availabilityOverlap struct {
	ID            string
	ParticipantID string
	StartAt       time.Time
	EndAt         time.Time
	Source        string
}

func (s *Store) blockConfirmedTimeInOtherEvents(ctx context.Context, tx pgx.Tx, eventID string, groupID *string, startAt, endAt time.Time) error {
	var groupIDValue any
	if groupID != nil {
		groupIDValue = *groupID
	}
	rows, err := tx.Query(ctx, `
		WITH source_users AS (
			SELECT DISTINCT p.user_id
			FROM participants p
			WHERE p.event_id = $1
				AND (
					$2::uuid IS NULL
					OR EXISTS (
						SELECT 1
						FROM scheduling_group_members gm
						WHERE gm.group_id = $2::uuid AND gm.participant_id = p.id
					)
				)
		)
		SELECT ai.id::text, ai.participant_id::text, ai.start_at, ai.end_at, ai.source
		FROM availability_intervals ai
		JOIN participants target_participant ON target_participant.id = ai.participant_id
		JOIN events target_event ON target_event.id = target_participant.event_id
		JOIN source_users ON source_users.user_id = target_participant.user_id
		WHERE target_event.id <> $1
			AND target_event.status = 'open'
			AND ai.start_at < $4
			AND ai.end_at > $3
		ORDER BY ai.participant_id, ai.start_at, ai.id
		FOR UPDATE OF ai
	`, eventID, groupIDValue, startAt, endAt)
	if err != nil {
		return err
	}
	overlaps := make([]availabilityOverlap, 0)
	for rows.Next() {
		var overlap availabilityOverlap
		if err := rows.Scan(&overlap.ID, &overlap.ParticipantID, &overlap.StartAt, &overlap.EndAt, &overlap.Source); err != nil {
			rows.Close()
			return err
		}
		overlaps = append(overlaps, overlap)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, overlap := range overlaps {
		remainders := subtractAvailabilityInterval(overlap.StartAt, overlap.EndAt, startAt, endAt)
		switch len(remainders) {
		case 0:
			if _, err := tx.Exec(ctx, `DELETE FROM availability_intervals WHERE id = $1`, overlap.ID); err != nil {
				return err
			}
		case 1:
			if _, err := tx.Exec(ctx, `
				UPDATE availability_intervals
				SET start_at = $2, end_at = $3, updated_at = CURRENT_TIMESTAMP
				WHERE id = $1
			`, overlap.ID, remainders[0].StartAt, remainders[0].EndAt); err != nil {
				return err
			}
		case 2:
			if _, err := tx.Exec(ctx, `
				UPDATE availability_intervals
				SET start_at = $2, end_at = $3, updated_at = CURRENT_TIMESTAMP
				WHERE id = $1
			`, overlap.ID, remainders[0].StartAt, remainders[0].EndAt); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO availability_intervals (participant_id, start_at, end_at, source)
				VALUES ($1, $2, $3, $4)
			`, overlap.ParticipantID, remainders[1].StartAt, remainders[1].EndAt, overlap.Source); err != nil {
				return err
			}
		}
	}
	return nil
}

func subtractAvailabilityInterval(startAt, endAt, unavailableStart, unavailableEnd time.Time) []availabilityRemainder {
	original := []availabilityRemainder{{StartAt: startAt, EndAt: endAt}}
	if !startAt.Before(endAt) || !unavailableStart.Before(unavailableEnd) {
		return original
	}
	if !startAt.Before(unavailableEnd) || !unavailableStart.Before(endAt) {
		return original
	}
	if !unavailableStart.After(startAt) && !unavailableEnd.Before(endAt) {
		return nil
	}
	if !unavailableStart.After(startAt) {
		return []availabilityRemainder{{StartAt: unavailableEnd, EndAt: endAt}}
	}
	if !unavailableEnd.Before(endAt) {
		return []availabilityRemainder{{StartAt: startAt, EndAt: unavailableStart}}
	}
	return []availabilityRemainder{
		{StartAt: startAt, EndAt: unavailableStart},
		{StartAt: unavailableEnd, EndAt: endAt},
	}
}
