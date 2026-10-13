ALTER TABLE events
    ADD COLUMN results_visible_to_participants BOOLEAN NOT NULL DEFAULT TRUE;
