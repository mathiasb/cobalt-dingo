-- Irreversible in the only sense that matters: the plaintext is gone and the
-- ciphertext cannot be decrypted by SQL, so rolling back drops the tokens
-- rather than restoring them. Every connection must be re-authorised.
ALTER TABLE fortnox_tokens
    DROP COLUMN access_token_sealed,
    DROP COLUMN refresh_token_sealed,
    DROP COLUMN refresh_token_fp;

DELETE FROM fortnox_tokens;

ALTER TABLE fortnox_tokens
    ADD COLUMN access_token  TEXT NOT NULL,
    ADD COLUMN refresh_token TEXT NOT NULL;
