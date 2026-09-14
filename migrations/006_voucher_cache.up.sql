-- Cache for Fortnox voucher rows (ADR-0006, #89).
--
-- Why this exists: GET /3/vouchers returns vouchers WITHOUT their rows despite
-- declaring VoucherRows in the OpenAPI spec. Rows come only from the
-- per-voucher detail endpoint, so row-level analysis costs one request per
-- voucher — roughly two minutes for a 405-voucher year at the documented rate
-- limit.
--
-- Why caching financial data is acceptable here: a posted voucher is
-- immutable. Swedish bookkeeping corrects by adding a further voucher, never by
-- editing or deleting one, so a cached voucher does not go stale. The hazard is
-- an INCOMPLETE cache reporting itself as whole, and that is detectable in one
-- request via Fortnox's own @TotalResources — see domain.VoucherCacheState.
--
-- This is a second place the books exist. It is derived and rebuildable and
-- must never be the source for a figure a human acts on without its as-of date
-- attached. Voucher descriptions name suppliers and customers, so treat these
-- rows as being as sensitive as the books themselves.

CREATE TABLE fortnox_voucher_cache (
    tenant_id        TEXT    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    year_id          INTEGER NOT NULL,
    series           TEXT    NOT NULL,
    number           INTEGER NOT NULL,
    transaction_date DATE    NOT NULL,
    description      TEXT    NOT NULL DEFAULT '',
    -- Rows as JSONB rather than a child table: they are read as a whole
    -- voucher, never queried independently, and a child table would invite
    -- exactly that — a row-level query against a cache whose completeness is
    -- only guaranteed per voucher.
    rows             JSONB   NOT NULL,
    PRIMARY KEY (tenant_id, year_id, series, number)
);

CREATE INDEX idx_voucher_cache_tenant_year
    ON fortnox_voucher_cache(tenant_id, year_id);

-- One row per (tenant, year): when the cache was last verified complete.
--
-- Separate from the cached vouchers on purpose. "How many vouchers are cached"
-- is a COUNT; "when was completeness last established" is a fact that has to
-- survive independently, because a cache with the right number of rows and no
-- record of having been checked is exactly the confident-but-unverified state
-- ADR-0006 refuses to serve.
CREATE TABLE fortnox_voucher_sync (
    tenant_id     TEXT        NOT NULL,
    year_id       INTEGER     NOT NULL,
    synced_at     TIMESTAMPTZ NOT NULL,
    -- Fortnox's @TotalResources at sync time, kept so a later read can tell a
    -- cache that was complete from one that was merely written.
    remote_total  INTEGER     NOT NULL,
    PRIMARY KEY (tenant_id, year_id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
