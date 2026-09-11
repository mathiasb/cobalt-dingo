package domain

import (
	"context"
	"errors"
)

// TenantID is the unique identifier for a cobalt-dingo tenant.
type TenantID string

// Tenant represents a customer company using cobalt-dingo.
type Tenant struct {
	ID   TenantID
	Name string
}

// DebtorAccount is the bank account a tenant uses to initiate payments.
// PISPHandle is populated once a PISP provider is integrated; empty until then.
type DebtorAccount struct {
	TenantID   TenantID
	Name       string // company name — used for PAIN.001 Dbtr/Nm
	IBAN       string
	BIC        string
	PISPHandle string // opaque account reference from the PISP provider
	IsDefault  bool
}

// ErrNoIntegration is returned by IntegrationStore.Get when this owner has
// registered no integration of their own. It is a normal state — the caller
// falls back to the application-level credentials — and is deliberately
// distinct from a store failure, which must not be read the same way.
var ErrNoIntegration = errors.New("no integration registered for this owner")

// Integration is a Fortnox connected app registered by one owner.
//
// Per ADR-0005 each tenant registers their own private Fortnox integration, so
// the client secret arrives from them and is stored encrypted at rest rather
// than injected from a vault.
type Integration struct {
	// OwnerID identifies whose integration this is.
	//
	// It must become the internal user ID from ADR-0003 rather than the OIDC
	// subject — #67 is that work, and this column is named neutrally so that
	// migration rewrites values, not the schema.
	OwnerID string

	// Mode is "sandbox" or "production". One integration per owner per mode:
	// a single Fortnox private integration is activated by many companies, so
	// this is not per-company.
	Mode string

	// ClientID is not a secret. Fortnox private integrations are activated by
	// a customer using the Client ID directly, so it is handed out by design.
	ClientID string

	// ClientSecretSealed is the client secret encrypted with internal/crypto —
	// base64(nonce || ciphertext). Never the plaintext, and never rendered back
	// to a browser.
	ClientSecretSealed string
}

// IntegrationStore reads owner-supplied Fortnox integrations.
//
// Read-only by design at this layer: writing one means accepting a secret from
// a request, which belongs behind its own deliberate path rather than being
// available anywhere this interface is.
type IntegrationStore interface {
	Get(ctx context.Context, ownerID, mode string) (Integration, error)
}
