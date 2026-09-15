package domain

import (
	"context"
	"errors"
	"time"
)

// Valid reports whether the access token is present and not expiring within 30 seconds.
func (t OAuthToken) Valid() bool {
	return t.AccessToken != "" && time.Now().Before(t.ExpiresAt.Add(-30*time.Second))
}

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
	UpsertTenant(ctx context.Context, t Tenant) error

	// ListByPrefix returns every tenant whose id starts with prefix. Tenant ids
	// are "<sub>:<mode>:<company>", so a prefix of "<sub>:<mode>:" is "every
	// company this user has connected in this mode" — which is what the company
	// switcher offers.
	ListByPrefix(ctx context.Context, prefix string) ([]Tenant, error)

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
	// Save unconditionally writes (upserts) a token for tenantID.
	Save(ctx context.Context, tenantID TenantID, token OAuthToken) error
	// AtomicRefresh replaces old with newToken only if old.RefreshToken still matches
	// the stored value. Returns ErrTokenConflict if another process won the race.
	AtomicRefresh(ctx context.Context, tenantID TenantID, old, newToken OAuthToken) error
	// Delete removes the token for tenantID. No-op if no token exists.
	Delete(ctx context.Context, tenantID TenantID) error
}

// VoucherCache stores voucher rows so row-level analysis does not cost one
// Fortnox request per voucher (ADR-0006, #89).
//
// Implementations: internal/adapter/postgres
//
// Deliberately NOT a general cache interface. It holds vouchers for a
// (tenant, financial year) because that is the unit Fortnox reports a total
// for — and the count comparison against that total is the only thing making
// a cache of financial data safe here.
type VoucherCache interface {
	// LoadYear returns the cached vouchers for a year and when the cache was
	// last verified complete. A zero time means never verified, which
	// VoucherCacheState.Assess treats as stale however many rows are present.
	//
	// It deliberately does NOT report freshness: that needs Fortnox's live
	// total, which is a network call and not this port's business. The caller
	// builds a VoucherCacheState and asks it.
	LoadYear(ctx context.Context, tenantID TenantID, yearID int) (CachedVouchers, error)

	// SaveYear replaces the cached set for a year and records the remote total
	// it was verified against.
	//
	// Replaces rather than merges: a merge cannot remove a voucher, and while
	// posted vouchers are immutable, a cache that can only grow could never
	// recover from a bad write. remoteTotal is stored so a later read can tell
	// a cache that was verified complete from one that was merely written.
	// SaveYear records VoucherFetchVersion itself rather than taking it as an
	// argument. It is a constant of the build doing the writing, and an
	// argument could be passed a stale value — which would defeat the check
	// silently, the same way the missing financialyear parameter did.
	SaveYear(ctx context.Context, tenantID TenantID, yearID int, vouchers []Voucher, remoteTotal int, syncedAt time.Time) error
}

// CachedVouchers is what the cache holds for one year, with the provenance
// needed to judge it.
//
// One value rather than three returns, for the same reason VoucherSet exists:
// SyncedAt and FetchVersion are not decoration, they are what makes the
// vouchers usable or not, and a caller cannot drop them by accident.
type CachedVouchers struct {
	Vouchers []Voucher

	// SyncedAt is when completeness was last verified. Zero means never.
	SyncedAt time.Time

	// FetchVersion is the VoucherFetchVersion that produced these rows. Zero
	// means they predate versioning and their provenance is unknown.
	FetchVersion int
}

// PaymentSubmitter initiates a payment batch via PSD2 PISP.
// Implementations: internal/adapter/tink (future), internal/adapter/nets (future)
type PaymentSubmitter interface {
	Submit(ctx context.Context, b Batch, account DebtorAccount) (SubmissionRef, error)
}

// SubmissionRef is the opaque reference returned by the PISP after submission.
type SubmissionRef string

// RefreshLock serialises OAuth token refreshes for one tenant across
// processes.
//
// Implementations: internal/adapter/postgres
//
// Exists because Fortnox rotates the refresh token on every use and detects
// reuse. Two processes that refresh concurrently both present the same refresh
// token, and Fortnox invalidates the entire family — which revoked this
// estate's production connection on 2026-09-14 and required a human to
// re-authorize at a consent screen.
//
// A compare-and-swap on the stored token cannot substitute for this: the swap
// happens after the network call, so the damage is already done.
type RefreshLock interface {
	// Acquire blocks until the caller may refresh this tenant's token. The
	// returned function must be called to release it.
	Acquire(ctx context.Context, tenantID TenantID) (release func(), err error)
}
