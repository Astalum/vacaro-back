UPDATE events
SET status = 'open'
WHERE scheduling_mode = 'groups' AND status = 'confirmed';

ALTER TABLE events DROP CONSTRAINT events_confirmation_check;
ALTER TABLE events ADD CONSTRAINT events_confirmation_check
    CHECK (
        (
            status = 'confirmed'
            AND confirmed_start_at IS NOT NULL
            AND confirmed_end_at IS NOT NULL
            AND confirmed_start_at < confirmed_end_at
            AND confirmed_start_at >= window_start_at
            AND confirmed_end_at <= window_end_at
        )
        OR (
            status <> 'confirmed'
            AND confirmed_start_at IS NULL
            AND confirmed_end_at IS NULL
        )
    );
ALTER TABLE events DROP CONSTRAINT events_scheduling_mode_check;
ALTER TABLE events DROP COLUMN scheduling_mode;
