-- name: GetTenant :one
SELECT id, name, created_at
FROM tenants
WHERE id = $1;

-- name: GetDefaultDebtorAccount :one
SELECT id, tenant_id, name, iban, bic, pisp_handle, is_default, created_at
FROM debtor_accounts
WHERE tenant_id = $1 AND is_default = TRUE;

-- name: ListTenantsByPrefix :many
-- Tenant ids are "<sub>:<mode>:<company>", so a prefix of "<sub>:<mode>:"
-- lists every company one user has connected in one mode. The prefix is
-- matched literally: LIKE metacharacters in it are escaped by the caller.
SELECT id, name, created_at
FROM tenants
WHERE id LIKE $1
ORDER BY name;

-- name: UpsertTenant :exec
INSERT INTO tenants (id, name)
VALUES ($1, $2)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name;

-- name: UpsertDebtorAccount :exec
INSERT INTO debtor_accounts (id, tenant_id, name, iban, bic, pisp_handle, is_default)
VALUES (gen_random_uuid()::text, $1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id) WHERE is_default = TRUE
DO UPDATE SET
    name        = EXCLUDED.name,
    iban        = EXCLUDED.iban,
    bic         = EXCLUDED.bic,
    pisp_handle = EXCLUDED.pisp_handle;

-- name: GetIntegration :one
SELECT owner_id, mode, client_id, client_secret_sealed
FROM fortnox_integrations
WHERE owner_id = $1 AND mode = $2;

-- name: UpsertIntegration :exec
INSERT INTO fortnox_integrations (owner_id, mode, client_id, client_secret_sealed)
VALUES ($1, $2, $3, $4)
ON CONFLICT (owner_id, mode) DO UPDATE SET
    client_id            = EXCLUDED.client_id,
    client_secret_sealed = EXCLUDED.client_secret_sealed,
    updated_at           = NOW();

-- name: DeleteIntegration :exec
DELETE FROM fortnox_integrations WHERE owner_id = $1 AND mode = $2;
