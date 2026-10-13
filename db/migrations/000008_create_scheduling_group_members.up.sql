CREATE TABLE scheduling_group_members (
    group_id UUID NOT NULL REFERENCES scheduling_groups (id) ON DELETE CASCADE,
    participant_id UUID NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, participant_id)
);

CREATE INDEX scheduling_group_members_participant_idx
    ON scheduling_group_members (participant_id);
