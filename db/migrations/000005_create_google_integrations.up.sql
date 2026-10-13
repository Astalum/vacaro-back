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
