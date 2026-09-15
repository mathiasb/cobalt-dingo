// Package fortnox provides adapters that implement domain ports using the Fortnox REST API.
package fortnox

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// Connector wraps the Fortnox HTTP client and implements domain.InvoiceSource
// and domain.SupplierEnricher. It uses the TokenStore for token lifecycle,
// including atomic refresh to prevent rolling-token race conditions.
type Connector struct {
	cfg    config.Fortnox
	tokens domain.TokenStore
	log    *slog.Logger
}

// NewConnector returns a Connector backed by the given config and token store.
func NewConnector(cfg config.Fortnox, tokens domain.TokenStore, log *slog.Logger) *Connector {
	return &Connector{cfg: cfg, tokens: tokens, log: log}
}

// UnpaidInvoices implements domain.InvoiceSource.
func (c *Connector) UnpaidInvoices(ctx context.Context, tenantID domain.TenantID) ([]domain.SupplierInvoice, error) {
	client, err := c.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("fortnox connector: %w", err)
	}
	invoices, err := client.UnpaidSupplierInvoices()
	if err != nil {
		return nil, fmt.Errorf("fortnox connector: %w", err)
	}
	return invoices, nil
}

// SupplierPaymentDetails implements domain.SupplierEnricher.
func (c *Connector) SupplierPaymentDetails(ctx context.Context, tenantID domain.TenantID, supplierNumber int) (iban, bic string, err error) {
	client, err := c.client(ctx, tenantID)
	if err != nil {
		return "", "", fmt.Errorf("fortnox connector: %w", err)
	}
	iban, bic, err = client.SupplierPaymentDetails(supplierNumber)
	if err != nil {
		return "", "", fmt.Errorf("fortnox connector: %w", err)
	}
	return iban, bic, nil
}

// client loads the token for tenantID and returns a ready-to-use Fortnox API
// client.
//
// It does NOT refresh. It used to, with its own copy of the refresh logic, and
// that duplication is what made the 2026-09-14 token revocation possible: a
// lock on one refresh path protects nothing while a second path refreshes
// unserialised. Freshness is now the token store's job — wrap the store in
// RefreshingTokenStore (with a RefreshLock) and every caller inherits one
// serialised implementation.
func (c *Connector) client(ctx context.Context, tenantID domain.TenantID) (*fortnox.Client, error) {
	tok, err := c.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return fortnox.NewClient(c.cfg.BaseURL(), tok.AccessToken, !c.cfg.AllowsWrites), nil
}
