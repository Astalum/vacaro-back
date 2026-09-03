CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by_user_id UUID NOT NULL,
    invite_token_hash CHAR(64) NOT NULL,
    title VARCHAR(200) NOT NULL,
    description TEXT,
    time_zone VARCHAR(100) NOT NULL,
    window_start_at TIMESTAMPTZ NOT NULL,
    window_end_at TIMESTAMPTZ NOT NULL,
    duration_minutes INTEGER NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'open',
    confirmed_start_at TIMESTAMPTZ,
    confirmed_end_at TIMESTAMPTZ,
    response_deadline_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT events_created_by_user_id_fkey
        FOREIGN KEY (created_by_user_id)
        REFERENCES users (id)
        ON DELETE RESTRICT,
    CONSTRAINT events_invite_token_hash_key
        UNIQUE (invite_token_hash),
    CONSTRAINT events_invite_token_hash_format_check
        CHECK (invite_token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT events_title_not_blank_check
        CHECK (btrim(title) <> ''),
    CONSTRAINT events_time_zone_not_blank_check
        CHECK (btrim(time_zone) <> ''),
    CONSTRAINT events_window_check
        CHECK (window_start_at < window_end_at),
    CONSTRAINT events_duration_minutes_check
        CHECK (duration_minutes > 0),
    CONSTRAINT events_status_check
        CHECK (status IN ('open', 'confirmed', 'cancelled')),
    CONSTRAINT events_confirmation_check
        CHECK (
            (
                status = 'confirmed'
                AND confirmed_start_at IS NOT NULL
                AND confirmed_end_at IS NOT NULL
                AND confirmed_start_at < confirmed_end_at
                AND confirmed_start_at >= window_start_at
                AND confirmed_end_at <= window_end_at
            )
            OR
            (
                status <> 'confirmed'
                AND confirmed_start_at IS NULL
                AND confirmed_end_at IS NULL
            )
        )
);

CREATE INDEX events_created_by_user_created_at_idx
    ON events (created_by_user_id, created_at DESC);

CREATE INDEX events_status_response_deadline_idx
    ON events (status, response_deadline_at);
