ALTER TABLE users
    ADD COLUMN availability_default VARCHAR(20) NOT NULL DEFAULT 'unavailable',
    ADD CONSTRAINT users_availability_default_check
        CHECK (availability_default IN ('available', 'unavailable'));
