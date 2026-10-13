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
