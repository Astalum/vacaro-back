ALTER TABLE events
    ADD COLUMN time_zone VARCHAR(100);

UPDATE events AS e
SET time_zone = u.time_zone
FROM users AS u
WHERE u.id = e.created_by_user_id;

ALTER TABLE events
    ALTER COLUMN time_zone SET NOT NULL,
    ADD CONSTRAINT events_time_zone_not_blank_check CHECK (btrim(time_zone) <> '');

ALTER TABLE users
    DROP CONSTRAINT users_time_zone_not_blank_check,
    DROP COLUMN time_zone;
