package fortnox

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// CustomerLedgerAdapter implements domain.CustomerLedger using the Fortnox REST API.
// It is intentionally thin: it loads a token, creates a raw client per call, and
// converts raw rows to domain types.
type CustomerLedgerAdapter struct {
	baseURL  string
	tokens   domain.TokenStore
	readOnly bool
}

// NewCustomerLedgerAdapter returns a CustomerLedgerAdapter pointed at
// baseURL and backed by the given token store. readOnly is propagated to
// the raw Fortnox client.
func NewCustomerLedgerAdapter(baseURL string, tokens domain.TokenStore, readOnly bool) *CustomerLedgerAdapter {
	return &CustomerLedgerAdapter{baseURL: baseURL, tokens: tokens, readOnly: readOnly}
}

// client loads the tenant's access token and returns a ready-to-use raw Fortnox client.
func (a *CustomerLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken, a.readOnly), nil
}

// UnpaidInvoices implements domain.CustomerLedger.
func (a *CustomerLedgerAdapter) UnpaidInvoices(ctx context.Context, tenantID domain.TenantID) ([]domain.CustomerInvoice, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("customer ledger: %w", err)
	}

	rows, err := c.UnpaidCustomerInvoices()
	if err != nil {
		return nil, fmt.Errorf("customer ledger: %w", err)
	}

	invoices := make([]domain.CustomerInvoice, len(rows))
	for i, row := range rows {
		customerNum, err := strconv.Atoi(row.CustomerNumber)
		if err != nil {
			return nil, fmt.Errorf("customer ledger: parse customer number %q: %w", row.CustomerNumber, err)
		}
		invoices[i] = domain.CustomerInvoice{
			InvoiceNumber:  row.DocumentNumber,
			CustomerNumber: customerNum,
			CustomerName:   row.CustomerName,
			Amount:         domain.MoneyFromFloat(row.Total, row.Currency),
			Balance:        domain.MoneyFromFloat(row.Balance, row.Currency),
			DueDate:        row.DueDate,
			InvoiceDate:    row.InvoiceDate,
			Booked:         row.Booked,
			Cancelled:      row.Cancelled,
			Sent:           row.Sent,
		}
	}
	return invoices, nil
}

// InvoicePayments implements domain.CustomerLedger.
func (a *CustomerLedgerAdapter) InvoicePayments(ctx context.Context, tenantID domain.TenantID, invoiceNumber int) ([]domain.CustomerPayment, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("customer ledger: %w", err)
	}

	rows, err := c.ListCustomerInvoicePayments(invoiceNumber)
	if err != nil {
		return nil, fmt.Errorf("customer ledger: %w", err)
	}

	payments := make([]domain.CustomerPayment, len(rows))
	for i, row := range rows {
		payments[i] = domain.CustomerPayment{
			PaymentNumber: row.Number,
			InvoiceNumber: row.InvoiceNumber,
			Amount:        domain.MoneyFromFloat(row.AmountCurrency, row.Currency),
			PaymentDate:   row.PaymentDate,
			Booked:        row.Booked,
		}
	}
	return payments, nil
}

// CustomerDetail implements domain.CustomerLedger.
func (a *CustomerLedgerAdapter) CustomerDetail(ctx context.Context, tenantID domain.TenantID, customerNumber int) (domain.Customer, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Customer{}, fmt.Errorf("customer ledger: %w", err)
	}

	row, err := c.GetFullCustomer(customerNumber)
	if err != nil {
		return domain.Customer{}, fmt.Errorf("customer ledger: %w", err)
	}

	custNum, err := strconv.Atoi(row.CustomerNumber)
	if err != nil {
		return domain.Customer{}, fmt.Errorf("customer ledger: parse customer number %q: %w", row.CustomerNumber, err)
	}

	return domain.Customer{
		CustomerNumber: custNum,
		Name:           row.Name,
		Email:          row.Email,
		Phone:          row.Phone,
		Active:         row.Active,
	}, nil
}
