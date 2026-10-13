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
