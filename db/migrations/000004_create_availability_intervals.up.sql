CREATE EXTENSION IF NOT EXISTS btree_gist;

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
