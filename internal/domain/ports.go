package domain

import (
	"context"
	"errors"
	"time"
)

// InvoiceSource retrieves unpaid supplier invoices for a tenant.
// Implementations: internal/adapter/fortnox (future)
type InvoiceSource interface {
	UnpaidInvoices(ctx context.Context, tenantID TenantID) ([]SupplierInvoice, error)
}

// SupplierEnricher fetches payment details (IBAN/BIC) for a supplier.
// Implementations: internal/adapter/fortnox (future)
type SupplierEnricher interface {
	SupplierPaymentDetails(ctx context.Context, tenantID TenantID, supplierNumber int) (iban, bic string, err error)
}

// BatchRepository persists and retrieves payment batches.
// Implementations: internal/adapter/postgres (future)
type BatchRepository interface {
	Save(ctx context.Context, b Batch) error
	Get(ctx context.Context, tenantID TenantID, id BatchID) (Batch, error)
	List(ctx context.Context, tenantID TenantID) ([]Batch, error)
}

// TenantRepository manages tenant and debtor account records.
// Implementations: internal/adapter/postgres (future)
type TenantRepository interface {
	Get(ctx context.Context, id TenantID) (Tenant, error)
	DefaultDebtorAccount(ctx context.Context, tenantID TenantID) (DebtorAccount, error)
}

// OAuthToken holds OAuth2 credentials and their expiry.
type OAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// ErrTokenConflict is returned by TokenStore.AtomicRefresh when another
// process already refreshed the token (rolling refresh race condition).
var ErrTokenConflict = errors.New("token conflict: refresh already performed by another process")

// TokenStore manages per-tenant OAuth tokens with atomic refresh semantics.
// AtomicRefresh prevents the rolling-refresh race: two concurrent requests both
// see an expired token, both call Fortnox, and the second invalidates the first.
// Implementations: internal/adapter/postgres (future)
type TokenStore interface {
	Load(ctx context.Context, tenantID TenantID) (OAuthToken, error)
	// AtomicRefresh replaces old with newToken only if old.RefreshToken still matches
	// the stored value. Returns ErrTokenConflict if another process won the race.
	AtomicRefresh(ctx context.Context, tenantID TenantID, old, newToken OAuthToken) error
}

// PaymentSubmitter initiates a payment batch via PSD2 PISP.
// Implementations: internal/adapter/tink (future), internal/adapter/nets (future)
type PaymentSubmitter interface {
	Submit(ctx context.Context, b Batch, account DebtorAccount) (SubmissionRef, error)
}

// SubmissionRef is the opaque reference returned by the PISP after submission.
type SubmissionRef string
