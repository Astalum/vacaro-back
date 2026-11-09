UPDATE availability_intervals SET source='manual' WHERE source='icloud_calendar';
ALTER TABLE availability_intervals DROP CONSTRAINT availability_intervals_source_check;
ALTER TABLE availability_intervals ADD CONSTRAINT availability_intervals_source_check CHECK (source IN ('manual','google_calendar'));
DROP TABLE icloud_calendar_syncs;
DROP TABLE icloud_calendar_connections;
