-- Reverse: rename {sub}:production back to {sub}:real_readonly.
BEGIN;

INSERT INTO tenants (id, name, created_at)
SELECT replace(id, ':production', ':real_readonly'), name, created_at
FROM tenants
WHERE id LIKE '%:production';

UPDATE fortnox_tokens
SET tenant_id = replace(tenant_id, ':production', ':real_readonly')
WHERE tenant_id LIKE '%:production';

UPDATE payment_batches
SET tenant_id = replace(tenant_id, ':production', ':real_readonly')
WHERE tenant_id LIKE '%:production';

UPDATE debtor_accounts
SET tenant_id = replace(tenant_id, ':production', ':real_readonly')
WHERE tenant_id LIKE '%:production';

DELETE FROM tenants WHERE id LIKE '%:production';

COMMIT;
