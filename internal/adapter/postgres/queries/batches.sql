-- name: InsertBatch :one
INSERT INTO payment_batches (id, tenant_id, msg_id, status, xml)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, tenant_id, msg_id, status, xml, created_at, submitted_at;

-- name: GetBatch :one
SELECT id, tenant_id, msg_id, status, xml, created_at, submitted_at
FROM payment_batches
WHERE tenant_id = $1 AND id = $2;

-- name: ListBatches :many
SELECT id, tenant_id, msg_id, status, xml, created_at, submitted_at
FROM payment_batches
WHERE tenant_id = $1
ORDER BY created_at DESC;

-- name: UpdateBatchStatus :exec
UPDATE payment_batches
SET status       = $2,
    submitted_at = CASE WHEN $2 = 'submitted' THEN NOW() ELSE submitted_at END
WHERE id = $1;

-- name: InsertBatchItem :exec
INSERT INTO batch_items (
    id, batch_id, fortnox_invoice_number, supplier_name,
    supplier_iban, supplier_bic, currency, amount_minor_units, due_date
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetBatchItems :many
SELECT id, batch_id, fortnox_invoice_number, supplier_name,
       supplier_iban, supplier_bic, currency, amount_minor_units,
       due_date, execution_rate
FROM batch_items
WHERE batch_id = $1;

-- name: SetExecutionRate :exec
UPDATE batch_items
SET execution_rate = $1
WHERE batch_id = $2 AND fortnox_invoice_number = $3;
