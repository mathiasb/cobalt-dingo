-- cobalt-dingo initial schema
-- Amounts stored as BIGINT minor units (cents/öre) — no floats in financial columns.
-- TODO: encrypt access_token and refresh_token columns before production deployment.

CREATE TABLE tenants (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE debtor_accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    iban        TEXT NOT NULL,
    bic         TEXT NOT NULL,
    pisp_handle TEXT,
    is_default  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Enforce at most one default debtor account per tenant at the DB level.
CREATE UNIQUE INDEX idx_debtor_accounts_one_default
    ON debtor_accounts(tenant_id) WHERE is_default = TRUE;

-- Fortnox OAuth2 tokens — one active row per tenant.
-- Rolling refresh: both tokens are replaced atomically on every refresh.
CREATE TABLE fortnox_tokens (
    tenant_id     TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    access_token  TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE payment_batches (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    msg_id       TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'draft',
    xml          BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMPTZ,
    CONSTRAINT payment_batches_msg_id_tenant_unique UNIQUE (tenant_id, msg_id),
    CONSTRAINT payment_batches_status_check
        CHECK (status IN ('draft', 'submitted', 'confirmed', 'reconciled'))
);

CREATE INDEX idx_payment_batches_tenant_id ON payment_batches(tenant_id);

CREATE TABLE batch_items (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id               UUID NOT NULL REFERENCES payment_batches(id) ON DELETE CASCADE,
    fortnox_invoice_number INTEGER NOT NULL,
    supplier_name          TEXT NOT NULL,
    supplier_iban          TEXT NOT NULL,
    supplier_bic           TEXT NOT NULL,
    currency               TEXT NOT NULL,
    amount_minor_units     BIGINT NOT NULL,
    due_date               DATE NOT NULL,
    -- Populated after bank payment confirmation; used to calculate FX delta voucher.
    execution_rate         NUMERIC(18,6),
    CONSTRAINT batch_items_unique_per_batch
        UNIQUE (batch_id, fortnox_invoice_number)
);

CREATE INDEX idx_batch_items_batch_id ON batch_items(batch_id);
