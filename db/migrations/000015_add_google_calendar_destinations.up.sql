CREATE TABLE google_calendar_destinations (
    user_id UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    calendar_id TEXT NOT NULL CHECK (btrim(calendar_id) <> '' AND lower(btrim(calendar_id)) <> 'primary'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- NULL identifies historical syncs into the primary calendar. They are retained
-- for history but must not be displayed as synced to the new destination.
ALTER TABLE confirmation_calendar_syncs ADD COLUMN calendar_id TEXT;
