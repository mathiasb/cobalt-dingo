// Package fortnox provides adapters that implement domain ports using the Fortnox REST API.
package fortnox

import (
	"context"
	"errors"
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

// client loads (and refreshes if necessary) the token for tenantID and returns
// a ready-to-use Fortnox API client.
func (c *Connector) client(ctx context.Context, tenantID domain.TenantID) (*fortnox.Client, error) {
	tok, err := c.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}

	if !tok.Valid() {
		newTok, err := c.refresh(tok.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("refresh token: %w", err)
		}

		if atomicErr := c.tokens.AtomicRefresh(ctx, tenantID, tok, newTok); atomicErr != nil {
			if errors.Is(atomicErr, domain.ErrTokenConflict) {
				// Another process already refreshed — reload and use the winner's token.
				c.log.Info("token refresh conflict: reloading", "tenant", tenantID)
				tok, err = c.tokens.Load(ctx, tenantID)
				if err != nil {
					return nil, fmt.Errorf("reload token after conflict: %w", err)
				}
			} else {
				return nil, fmt.Errorf("persist refreshed token: %w", atomicErr)
			}
		} else {
			tok = newTok
		}
	}

	return fortnox.NewClient(c.cfg.BaseURL(), tok.AccessToken, !c.cfg.Mode.AllowsWrites()), nil
}

// refresh calls the Fortnox token endpoint and converts the result to domain.OAuthToken.
func (c *Connector) refresh(refreshToken string) (domain.OAuthToken, error) {
	t, err := fortnox.RefreshAccessToken(c.cfg.ClientID, c.cfg.ClientSecret, refreshToken)
	if err != nil {
		return domain.OAuthToken{}, err
	}
	return domain.OAuthToken{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    t.ExpiresAt,
	}, nil
}
