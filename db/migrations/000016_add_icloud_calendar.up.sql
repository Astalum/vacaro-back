CREATE TABLE icloud_calendar_connections (
 user_id UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
 credentials_ciphertext BYTEA NOT NULL,
 calendar_id TEXT NOT NULL,
 connected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE icloud_calendar_syncs (
 event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
 group_id UUID REFERENCES scheduling_groups (id) ON DELETE CASCADE,
 user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
 calendar_id TEXT,
 status VARCHAR(20) NOT NULL CHECK (status IN ('synced','failed')),
 attempted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX icloud_calendar_syncs_whole_key ON icloud_calendar_syncs(event_id,user_id) WHERE group_id IS NULL;
CREATE UNIQUE INDEX icloud_calendar_syncs_group_key ON icloud_calendar_syncs(event_id,group_id,user_id) WHERE group_id IS NOT NULL;
CREATE INDEX icloud_calendar_syncs_user_event_idx ON icloud_calendar_syncs(user_id,event_id);
ALTER TABLE availability_intervals DROP CONSTRAINT availability_intervals_source_check;
ALTER TABLE availability_intervals ADD CONSTRAINT availability_intervals_source_check CHECK (source IN ('manual','google_calendar','icloud_calendar'));
