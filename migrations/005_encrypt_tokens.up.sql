-- Encrypt Fortnox tokens at rest — #85, closing the TODO from 001_initial.
--
-- access_token and refresh_token were plaintext columns. An access token IS
-- access to the books, and a refresh token mints more for 45 days, so this is
-- the more valuable secret in the schema — more so than the client secret
-- internal/crypto was built for.
--
-- WHY THE ROWS ARE DELETED RATHER THAN MIGRATED: SQL cannot encrypt. There is
-- no expression available here that produces AES-GCM ciphertext, so existing
-- plaintext cannot be converted in place. The alternative — teaching the
-- application to read both shapes and re-seal on next write — leaves plaintext
-- in the table until each row happens to be touched, and adds a dual-read path
-- that must then be removed later.
--
-- Deleting is acceptable *specifically here* because the only stored token is
-- the sandbox connection, and it has been unreachable since the credential key
-- gained a company component. Reconnecting is one click and lands under the
-- correct key first time. On a table with live production tokens this would be
-- the wrong migration.
-- DEPLOY ORDER MATTERS, and this migration cannot enforce it.
--
-- Dropping the plaintext columns breaks the RUNNING pod immediately: its
-- SELECT names columns that no longer exist. And applying it after the new
-- image has rolled means the new pod queries columns that do not exist yet.
-- Either way there is a window.
--
-- Acceptable here because the window is one pod on one node with one
-- already-unreachable token, and because the correct sequence is short:
--
--   1. merge and let the image build
--   2. apply this migration
--   3. roll the deployment (or let Flux do it — the image tag bump is what
--      triggers it, so in practice 2 and 3 are seconds apart)
--   4. reconnect the sandbox company once, in the UI
--
-- On a system where that window is not acceptable, the expand-contract shape
-- is the answer: add the sealed columns nullable, deploy code that writes both
-- and reads sealed-then-plaintext, backfill, then drop. That is three deploys
-- to avoid a few seconds here, which is why it is not what this does.
DELETE FROM fortnox_tokens;

ALTER TABLE fortnox_tokens
    DROP COLUMN access_token,
    DROP COLUMN refresh_token;

ALTER TABLE fortnox_tokens
    -- AES-256-GCM via internal/crypto, base64(nonce || ciphertext).
    ADD COLUMN access_token_sealed  TEXT NOT NULL,
    ADD COLUMN refresh_token_sealed TEXT NOT NULL,

    -- Deterministic keyed fingerprint (HMAC-SHA256, its own derived key).
    --
    -- The rolling-refresh race is detected by a compare-and-swap on the
    -- refresh token. GCM ciphertext is randomised, so the same token seals
    -- differently every time and `WHERE refresh_token_sealed = $1` would never
    -- match — the race detection would silently stop working, which is worse
    -- than the plaintext it replaces: two processes would both believe they
    -- won. The CAS matches on this column instead.
    ADD COLUMN refresh_token_fp     TEXT NOT NULL;
