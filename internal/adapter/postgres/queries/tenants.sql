-- name: GetTenant :one
SELECT id, name, created_at
FROM tenants
WHERE id = $1;

-- name: GetDefaultDebtorAccount :one
SELECT id, tenant_id, name, iban, bic, pisp_handle, is_default, created_at
FROM debtor_accounts
WHERE tenant_id = $1 AND is_default = TRUE;

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
