-- Dropping the column loses only the provenance of a derived, rebuildable
-- cache. Note that a binary running WITHOUT this column cannot tell poisoned
-- rows from good ones, which is the state this migration exists to end.
ALTER TABLE fortnox_voucher_sync
    DROP COLUMN IF EXISTS fetch_version;
