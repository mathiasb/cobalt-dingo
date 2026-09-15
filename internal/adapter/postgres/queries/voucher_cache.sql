-- name: UpsertCachedVoucher :exec
-- Rows arrive as JSONB. Upsert rather than insert so a resync of an unchanged
-- voucher is a no-op rather than a conflict.
INSERT INTO fortnox_voucher_cache (
    tenant_id, year_id, series, number, transaction_date, description, rows
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id, year_id, series, number) DO UPDATE
SET transaction_date = EXCLUDED.transaction_date,
    description      = EXCLUDED.description,
    rows             = EXCLUDED.rows;

-- name: DeleteCachedYear :exec
-- SaveYear replaces rather than merges: a merge cannot remove a voucher, so a
-- bad write would be permanent.
DELETE FROM fortnox_voucher_cache WHERE tenant_id = $1 AND year_id = $2;

-- name: ListCachedVouchers :many
-- Ordered so a cached answer is byte-identical between reads; an unstable
-- order makes two correct answers look like a discrepancy.
SELECT series, number, transaction_date, description, rows
FROM fortnox_voucher_cache
WHERE tenant_id = $1 AND year_id = $2
ORDER BY transaction_date, series, number;

-- name: UpsertVoucherSync :exec
-- fetch_version records which code produced the rows. Without it, a change to
-- how rows are read leaves the count identical and every cached row wrong —
-- see domain.VoucherFetchVersion.
INSERT INTO fortnox_voucher_sync (tenant_id, year_id, synced_at, remote_total, fetch_version)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id, year_id) DO UPDATE
SET synced_at     = EXCLUDED.synced_at,
    remote_total  = EXCLUDED.remote_total,
    fetch_version = EXCLUDED.fetch_version;

-- name: GetVoucherSync :one
SELECT synced_at, remote_total, fetch_version
FROM fortnox_voucher_sync
WHERE tenant_id = $1 AND year_id = $2;

-- name: ListUnbookedSeen :many
-- Ordered for stable output; the caller compares sets, but a stable read makes
-- two identical states produce identical logs.
SELECT kind, invoice_number
FROM fortnox_unbooked_seen
WHERE tenant_id = $1
ORDER BY kind, invoice_number;

-- name: RememberUnbooked :exec
-- first_seen_at is kept on conflict: it records when the obligation was FIRST
-- noticed, which is the useful fact. Overwriting it on every run would turn it
-- into "last seen" and lose how long something has been outstanding.
INSERT INTO fortnox_unbooked_seen (tenant_id, kind, invoice_number, first_seen_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, kind, invoice_number) DO NOTHING;

-- name: ForgetUnbooked :exec
DELETE FROM fortnox_unbooked_seen
WHERE tenant_id = $1 AND kind = $2 AND invoice_number = $3;
