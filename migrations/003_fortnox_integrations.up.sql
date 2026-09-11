-- Per-owner Fortnox connected apps — ADR-0005.
--
-- Each tenant registers their own private Fortnox integration and supplies its
-- client id and secret, which removes Fortnox from the approval path: a public
-- integration needs publication, a partner agreement and a vendor review, while
-- a private one works the moment the tenant creates it.
--
-- One row per (owner, mode), NOT per company: a single Fortnox private
-- integration is activated by many companies using its Client ID, so the
-- credential is not per-company. Company scoping lives in fortnox_tokens.
CREATE TABLE fortnox_integrations (
    -- Whose integration this is.
    --
    -- Must become the internal user ID from ADR-0003 rather than the OIDC
    -- subject; #67 is that work. Named neutrally so that migration rewrites
    -- values rather than the schema.
    owner_id             TEXT NOT NULL,

    mode                 TEXT NOT NULL CHECK (mode IN ('sandbox', 'production')),

    -- Not a secret: Fortnox private integrations are activated by a customer
    -- using the Client ID directly, so it is handed out by design.
    client_id            TEXT NOT NULL CHECK (client_id <> ''),

    -- AES-256-GCM via internal/crypto, stored as base64(nonce || ciphertext).
    -- The master key lives in 1Password AgentSecrets and never in this database.
    -- A plaintext column here would be a worse posture than the ESO-injected
    -- environment variable this replaces.
    client_secret_sealed TEXT NOT NULL CHECK (client_secret_sealed <> ''),

    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (owner_id, mode)
);

-- No foreign key to tenants: tenants.id is "<sub>:<mode>:<company>", a
-- different grain from owner_id. Referencing it would force a company to exist
-- before its integration can, which inverts the actual order of events.
