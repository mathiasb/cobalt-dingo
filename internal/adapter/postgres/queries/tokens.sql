-- name: GetToken :one
SELECT access_token, refresh_token, expires_at
FROM fortnox_tokens
WHERE tenant_id = $1;

-- name: UpsertToken :exec
INSERT INTO fortnox_tokens (tenant_id, access_token, refresh_token, expires_at, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (tenant_id) DO UPDATE SET
    access_token  = EXCLUDED.access_token,
    refresh_token = EXCLUDED.refresh_token,
    expires_at    = EXCLUDED.expires_at,
    updated_at    = NOW();

-- Replaces the token only when the stored refresh_token still matches old_refresh_token.
-- Returns the tenant_id row when successful; zero rows means ErrTokenConflict.
-- name: AtomicRefreshToken :one
UPDATE fortnox_tokens
SET access_token  = $3,
    refresh_token = $4,
    expires_at    = $5,
    updated_at    = NOW()
WHERE tenant_id = $1 AND refresh_token = $2
RETURNING tenant_id;
