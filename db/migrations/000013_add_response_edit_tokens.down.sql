DROP INDEX participants_response_edit_token_hash_key;

ALTER TABLE participants
    DROP COLUMN response_edit_token_hash;
