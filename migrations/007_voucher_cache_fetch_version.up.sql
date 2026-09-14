-- Records WHICH code produced the cached rows (domain.VoucherFetchVersion).
--
-- Why: the completeness check counts vouchers and can say nothing about their
-- content. On 2026-09-14 the voucher detail fetch was missing its
-- financialyear parameter, so four of 405 vouchers were stored with rows
-- belonging to other vouchers. 405 == 405, so the cache reported itself
-- verified complete and would have served those rows indefinitely. No count,
-- timestamp or remote total can detect that.
--
-- DEFAULT 0 is deliberate and is not a neutral value: 0 means "written before
-- versioning existed", which domain.VoucherCacheState.Assess treats as stale.
-- Existing rows therefore invalidate themselves, which is the correct outcome
-- here — the rows in this table right now are the poisoned ones.
ALTER TABLE fortnox_voucher_sync
    ADD COLUMN fetch_version INTEGER NOT NULL DEFAULT 0;
