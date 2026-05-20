-- Rename tenant IDs from {sub}:real_readonly to {sub}:production.
-- Uses insert-update-delete to respect non-deferrable FK constraints on tenants PK.
BEGIN;

-- 1. Insert new tenant rows with updated IDs (no FK violation — PK insert).
INSERT INTO tenants (id, name, created_at)
SELECT replace(id, ':real_readonly', ':production'), name, created_at
FROM tenants
WHERE id LIKE '%:real_readonly';

-- 2. Migrate child tables to reference the new tenant IDs.
UPDATE fortnox_tokens
SET tenant_id = replace(tenant_id, ':real_readonly', ':production')
WHERE tenant_id LIKE '%:real_readonly';

UPDATE payment_batches
SET tenant_id = replace(tenant_id, ':real_readonly', ':production')
WHERE tenant_id LIKE '%:real_readonly';

UPDATE debtor_accounts
SET tenant_id = replace(tenant_id, ':real_readonly', ':production')
WHERE tenant_id LIKE '%:real_readonly';

-- 3. Remove old tenant rows (children no longer reference them).
DELETE FROM tenants WHERE id LIKE '%:real_readonly';

COMMIT;
