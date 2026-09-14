-- Dropping the cache loses nothing: it is derived from Fortnox and rebuildable
-- by re-reading the vouchers. That is the property that makes it a cache.
DROP TABLE IF EXISTS fortnox_voucher_sync;
DROP TABLE IF EXISTS fortnox_voucher_cache;
