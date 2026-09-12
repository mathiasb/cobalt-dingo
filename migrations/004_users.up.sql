-- Internal user identity — ADR-0003.
--
-- Credentials are keyed by an ID this application owns, not by the OIDC
-- subject. The subject is provider-scoped and opaque: Authentik's `sub_mode`
-- alone changes its shape, and recreating a provider can change it with no
-- migration involved. Keying credentials on it orphaned a live Fortnox token
-- during the Dex→Authentik migration — the credential was not deleted, it sat
-- under a key nobody would ever compute again, while the UI reported success.
CREATE TABLE users (
    id         TEXT PRIMARY KEY,

    -- The resolution input. Lowercased before storage so a differently-cased
    -- login cannot mint a second user for the same human.
    --
    -- Email is NOT the stored key: addresses change, and every credential row
    -- keyed by one would then need rewriting — the same migration ADR-0003
    -- exists to avoid, merely deferred. An ID resolved FROM email keeps the
    -- stable key stable and makes an email change a one-row update.
    email      TEXT NOT NULL UNIQUE CHECK (email = lower(email)),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Every OIDC subject ever seen for a user. Audit and debugging only: this
-- table exists so that "which subject did this user present, and when" is
-- answerable after an IdP change, which is precisely what was unanswerable
-- when the token was orphaned.
--
-- Never a lookup key for credentials. That is the whole decision.
CREATE TABLE user_subjects (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sub        TEXT NOT NULL,
    issuer     TEXT NOT NULL DEFAULT '',
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id, sub)
);
