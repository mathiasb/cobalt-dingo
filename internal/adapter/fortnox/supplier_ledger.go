package fortnox

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// SupplierLedgerAdapter implements domain.SupplierLedger using the Fortnox REST API.
// It is intentionally thin: it loads a token, creates a raw client per call, and
// converts raw rows to domain types. No retry or refresh logic — callers that need
// refresh should wrap the TokenStore or use the full Connector.
type SupplierLedgerAdapter struct {
	baseURL  string
	tokens   domain.TokenStore
	readOnly bool
}

// NewSupplierLedgerAdapter returns a SupplierLedgerAdapter pointed at baseURL,
// backed by the given token store. readOnly is propagated to the raw Fortnox
// client so writes are refused locally when this adapter is used in a
// read-only mode.
func NewSupplierLedgerAdapter(baseURL string, tokens domain.TokenStore, readOnly bool) *SupplierLedgerAdapter {
	return &SupplierLedgerAdapter{baseURL: baseURL, tokens: tokens, readOnly: readOnly}
}

// client loads the tenant's access token and returns a ready-to-use raw Fortnox client.
func (a *SupplierLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken, a.readOnly), nil
}

// UnpaidInvoices implements domain.SupplierLedger.
func (a *SupplierLedgerAdapter) UnpaidInvoices(ctx context.Context, tenantID domain.TenantID) ([]domain.SupplierInvoice, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("supplier ledger unpaid invoices: %w", err)
	}
	invoices, err := c.UnpaidSupplierInvoices()
	if err != nil {
		return nil, fmt.Errorf("supplier ledger unpaid invoices: %w", err)
	}
	return invoices, nil
}

// InvoicePayments implements domain.SupplierLedger.
func (a *SupplierLedgerAdapter) InvoicePayments(ctx context.Context, tenantID domain.TenantID, invoiceNumber int) ([]domain.SupplierPayment, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("supplier ledger invoice payments: %w", err)
	}
	rows, err := c.ListSupplierInvoicePayments(invoiceNumber)
	if err != nil {
		return nil, fmt.Errorf("supplier ledger invoice payments: %w", err)
	}

	payments := make([]domain.SupplierPayment, len(rows))
	for i, r := range rows {
		payments[i] = domain.SupplierPayment{
			PaymentNumber: r.Number,
			InvoiceNumber: r.InvoiceNumber,
			Amount:        domain.MoneyFromFloat(r.AmountCurrency, r.Currency),
			CurrencyRate:  r.CurrencyRate,
			PaymentDate:   r.PaymentDate,
			Booked:        r.Booked,
		}
	}
	return payments, nil
}

// SupplierDetail implements domain.SupplierLedger.
func (a *SupplierLedgerAdapter) SupplierDetail(ctx context.Context, tenantID domain.TenantID, supplierNumber int) (domain.Supplier, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Supplier{}, fmt.Errorf("supplier ledger supplier detail: %w", err)
	}
	row, err := c.GetFullSupplier(supplierNumber)
	if err != nil {
		return domain.Supplier{}, fmt.Errorf("supplier ledger supplier detail: %w", err)
	}
	return domain.Supplier{
		SupplierNumber: row.SupplierNumber,
		Name:           row.Name,
		Email:          row.Email,
		Phone:          row.Phone,
		IBAN:           row.IBAN,
		BIC:            row.BIC,
		Active:         row.Active,
	}, nil
}
