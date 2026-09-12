-- Dropping this table loses every tenant-supplied client secret. They are not
-- recoverable from anywhere else: the plaintext exists only in the tenant's own
-- Fortnox developer portal, so each affected tenant must re-enter theirs.
DROP TABLE IF EXISTS fortnox_integrations;
