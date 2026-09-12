CREATE TABLE scheduling_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    duration_minutes INTEGER NOT NULL,
    confirmed_start_at TIMESTAMPTZ,
    confirmed_end_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT scheduling_groups_name_not_blank_check CHECK (btrim(name) <> ''),
    CONSTRAINT scheduling_groups_event_name_key UNIQUE (event_id, name),
    CONSTRAINT scheduling_groups_duration_check CHECK (duration_minutes > 0),
    CONSTRAINT scheduling_groups_confirmation_check CHECK (
        (confirmed_start_at IS NULL AND confirmed_end_at IS NULL)
        OR (
            confirmed_start_at IS NOT NULL
            AND confirmed_end_at IS NOT NULL
            AND confirmed_start_at < confirmed_end_at
        )
    )
);

CREATE INDEX scheduling_groups_event_created_at_idx
    ON scheduling_groups (event_id, created_at, id);
