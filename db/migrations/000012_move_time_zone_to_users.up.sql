ALTER TABLE users
    ADD COLUMN time_zone VARCHAR(100);

UPDATE users AS u
SET time_zone = COALESCE(
    (
        SELECT e.time_zone
        FROM events AS e
        WHERE e.created_by_user_id = u.id
        ORDER BY e.updated_at DESC, e.id DESC
        LIMIT 1
    ),
    'UTC'
);

ALTER TABLE users
    ALTER COLUMN time_zone SET DEFAULT 'UTC',
    ALTER COLUMN time_zone SET NOT NULL,
    ADD CONSTRAINT users_time_zone_not_blank_check CHECK (btrim(time_zone) <> '');

ALTER TABLE events
    DROP CONSTRAINT events_time_zone_not_blank_check,
    DROP COLUMN time_zone;
