-- name: GetToken :one
SELECT access_token_sealed, refresh_token_sealed, expires_at
FROM fortnox_tokens
WHERE tenant_id = $1;

-- name: UpsertToken :exec
INSERT INTO fortnox_tokens (tenant_id, access_token_sealed, refresh_token_sealed, refresh_token_fp, expires_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (tenant_id) DO UPDATE SET
    access_token_sealed  = EXCLUDED.access_token_sealed,
    refresh_token_sealed = EXCLUDED.refresh_token_sealed,
    refresh_token_fp     = EXCLUDED.refresh_token_fp,
    expires_at           = EXCLUDED.expires_at,
    updated_at           = NOW();

-- Replaces the token only when the stored refresh token still matches the one
-- the caller read. Matched on the FINGERPRINT, not the ciphertext: GCM is
-- randomised, so comparing sealed values would never match and the race
-- detection would silently stop working.
-- Returns the tenant_id row when successful; zero rows means ErrTokenConflict.
-- name: AtomicRefreshToken :one
UPDATE fortnox_tokens
SET access_token_sealed  = $3,
    refresh_token_sealed = $4,
    refresh_token_fp     = $5,
    expires_at           = $6,
    updated_at           = NOW()
WHERE tenant_id = $1 AND refresh_token_fp = $2
RETURNING tenant_id;

-- name: DeleteToken :exec
DELETE FROM fortnox_tokens WHERE tenant_id = $1;
