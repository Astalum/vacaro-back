ALTER TABLE participants
    ADD COLUMN response_edit_token_hash VARCHAR(64);

CREATE UNIQUE INDEX participants_response_edit_token_hash_key
    ON participants (response_edit_token_hash)
    WHERE response_edit_token_hash IS NOT NULL;
