CREATE TABLE participants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL,
    user_id UUID NOT NULL,
    display_name VARCHAR(100) NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'member',
    response_status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT participants_event_id_fkey
        FOREIGN KEY (event_id)
        REFERENCES events (id)
        ON DELETE CASCADE,
    CONSTRAINT participants_user_id_fkey
        FOREIGN KEY (user_id)
        REFERENCES users (id)
        ON DELETE RESTRICT,
    CONSTRAINT participants_event_user_key
        UNIQUE (event_id, user_id),
    CONSTRAINT participants_display_name_not_blank_check
        CHECK (btrim(display_name) <> ''),
    CONSTRAINT participants_role_check
        CHECK (role IN ('organizer', 'member')),
    CONSTRAINT participants_response_status_check
        CHECK (response_status IN ('pending', 'submitted'))
);

CREATE UNIQUE INDEX participants_one_organizer_per_event_idx
    ON participants (event_id)
    WHERE role = 'organizer';

CREATE INDEX participants_user_id_idx
    ON participants (user_id);
