ALTER TABLE events DROP CONSTRAINT events_confirmation_check;
ALTER TABLE events ADD CONSTRAINT events_confirmation_check
    CHECK (
        (
            scheduling_mode = 'whole_event'
            AND (
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
            )
        )
        OR (
            scheduling_mode = 'groups'
            AND confirmed_start_at IS NULL
            AND confirmed_end_at IS NULL
        )
    );
