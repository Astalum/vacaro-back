CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_type VARCHAR(20) NOT NULL DEFAULT 'guest',
    display_name VARCHAR(100) NOT NULL,
    email VARCHAR(320),
    guest_expires_at TIMESTAMPTZ,
    anonymized_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT users_account_type_check
        CHECK (account_type IN ('guest', 'registered')),
    CONSTRAINT users_display_name_not_blank_check
        CHECK (btrim(display_name) <> ''),
    CONSTRAINT users_guest_expiration_check
        CHECK (
            (account_type = 'guest' AND guest_expires_at IS NOT NULL)
            OR
            (account_type = 'registered' AND guest_expires_at IS NULL)
        )
);

CREATE INDEX users_active_guest_expiration_idx
    ON users (guest_expires_at)
    WHERE account_type = 'guest' AND anonymized_at IS NULL;

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
    scheduling_mode VARCHAR(20) NOT NULL DEFAULT 'whole_event',
    status VARCHAR(20) NOT NULL DEFAULT 'open',
    confirmed_start_at TIMESTAMPTZ,
    confirmed_end_at TIMESTAMPTZ,
    response_deadline_at TIMESTAMPTZ,
    results_visible_to_participants BOOLEAN NOT NULL DEFAULT TRUE,
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
    CONSTRAINT events_scheduling_mode_check
        CHECK (scheduling_mode IN ('whole_event', 'groups')),
    CONSTRAINT events_status_check
        CHECK (status IN ('open', 'confirmed', 'cancelled')),
    CONSTRAINT events_confirmation_check
        CHECK (
            (
                confirmed_start_at IS NULL
                AND confirmed_end_at IS NULL
            )
            OR (
                status = 'confirmed'
                AND confirmed_start_at IS NOT NULL
                AND confirmed_end_at IS NOT NULL
                AND confirmed_start_at < confirmed_end_at
                AND confirmed_start_at >= window_start_at
                AND confirmed_end_at <= window_end_at
            )
        )
);

CREATE INDEX events_created_by_user_created_at_idx
    ON events (created_by_user_id, created_at DESC);

CREATE INDEX events_status_response_deadline_idx
    ON events (status, response_deadline_at);

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

CREATE TABLE availability_intervals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    participant_id UUID NOT NULL,
    start_at TIMESTAMPTZ NOT NULL,
    end_at TIMESTAMPTZ NOT NULL,
    source VARCHAR(20) NOT NULL DEFAULT 'manual',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT availability_intervals_participant_id_fkey
        FOREIGN KEY (participant_id)
        REFERENCES participants (id)
        ON DELETE CASCADE,
    CONSTRAINT availability_intervals_time_range_check
        CHECK (start_at < end_at),
    CONSTRAINT availability_intervals_source_check
        CHECK (source IN ('manual', 'google_calendar')),
    CONSTRAINT availability_intervals_no_overlap
        EXCLUDE USING gist (
            participant_id WITH =,
            tstzrange(start_at, end_at, '[)') WITH &&
        )
        DEFERRABLE INITIALLY IMMEDIATE
);

CREATE INDEX availability_intervals_participant_start_idx
    ON availability_intervals (participant_id, start_at);

CREATE TABLE auth_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    provider VARCHAR(30) NOT NULL,
    provider_subject VARCHAR(255) NOT NULL,
    email VARCHAR(320),
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT auth_accounts_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT auth_accounts_provider_subject_key
        UNIQUE (provider, provider_subject),
    CONSTRAINT auth_accounts_user_provider_key
        UNIQUE (user_id, provider),
    CONSTRAINT auth_accounts_provider_check
        CHECK (provider IN ('google')),
    CONSTRAINT auth_accounts_provider_subject_not_blank_check
        CHECK (btrim(provider_subject) <> '')
);

CREATE INDEX auth_accounts_user_id_idx ON auth_accounts (user_id);

CREATE TABLE google_calendar_connections (
    auth_account_id UUID PRIMARY KEY,
    refresh_token_ciphertext BYTEA NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    connected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT google_calendar_connections_auth_account_id_fkey
        FOREIGN KEY (auth_account_id) REFERENCES auth_accounts (id) ON DELETE CASCADE
);

CREATE TABLE scheduling_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    duration_minutes INTEGER NOT NULL,
    confirmed_start_at TIMESTAMPTZ,
    confirmed_end_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT scheduling_groups_name_not_blank_check
        CHECK (btrim(name) <> ''),
    CONSTRAINT scheduling_groups_event_name_key
        UNIQUE (event_id, name),
    CONSTRAINT scheduling_groups_duration_check
        CHECK (duration_minutes > 0),
    CONSTRAINT scheduling_groups_confirmation_check
        CHECK (
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

CREATE TABLE scheduling_group_members (
    group_id UUID NOT NULL REFERENCES scheduling_groups (id) ON DELETE CASCADE,
    participant_id UUID NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, participant_id)
);

CREATE INDEX scheduling_group_members_participant_idx
    ON scheduling_group_members (participant_id);

CREATE TABLE confirmation_calendar_syncs (
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    group_id UUID REFERENCES scheduling_groups (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT confirmation_calendar_syncs_status_check
        CHECK (status IN ('synced', 'failed'))
);

CREATE UNIQUE INDEX confirmation_calendar_syncs_whole_event_key
    ON confirmation_calendar_syncs (event_id, user_id)
    WHERE group_id IS NULL;

CREATE UNIQUE INDEX confirmation_calendar_syncs_group_key
    ON confirmation_calendar_syncs (event_id, group_id, user_id)
    WHERE group_id IS NOT NULL;

CREATE INDEX confirmation_calendar_syncs_user_event_idx
    ON confirmation_calendar_syncs (user_id, event_id);
