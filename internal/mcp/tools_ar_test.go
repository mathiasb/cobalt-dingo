package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCustomerLedger implements domain.CustomerLedger with a fixed set of
// unpaid invoices. Only UnpaidInvoices is exercised by ar_by_customer.
type fakeCustomerLedger struct {
	invoices []domain.CustomerInvoice
}

func (f fakeCustomerLedger) UnpaidInvoices(_ context.Context, _ domain.TenantID) ([]domain.CustomerInvoice, error) {
	return f.invoices, nil
}

func (f fakeCustomerLedger) StateCounts(_ context.Context, _ domain.TenantID) ([]domain.InvoiceStateCount, error) {
	return nil, nil
}

func (f fakeCustomerLedger) UnbookedInvoices(_ context.Context, _ domain.TenantID) ([]domain.CustomerInvoice, error) {
	return nil, nil
}

func (f fakeCustomerLedger) InvoicePayments(_ context.Context, _ domain.TenantID, _ int) ([]domain.CustomerPayment, error) {
	return nil, nil
}

func (f fakeCustomerLedger) CustomerDetail(_ context.Context, _ domain.TenantID, _ int) (domain.Customer, error) {
	return domain.Customer{}, nil
}

// TestARByCustomerReturnsPerCurrencyTotals verifies that ar_by_customer never
// collapses amounts across currencies into a single figure: a customer with
// EUR 2500.00 and SEK 1200.00 unpaid must report one total per currency, and
// the result must not carry a total_sek field — no FX conversion happens, so
// a single SEK total would be fabricated.
func TestARByCustomerReturnsPerCurrencyTotals(t *testing.T) {
	deps := mcpserver.Deps{
		TenantID: domain.TenantID("test"),
		CustomerLdg: fakeCustomerLedger{invoices: []domain.CustomerInvoice{
			{InvoiceNumber: 1, CustomerNumber: 5, CustomerName: "Kund AB", Amount: domain.Money{MinorUnits: 250000, Currency: "EUR"}, DueDate: "2026-01-01"},
			{InvoiceNumber: 2, CustomerNumber: 5, CustomerName: "Kund AB", Amount: domain.Money{MinorUnits: 120000, Currency: "SEK"}, DueDate: "2026-01-02"},
		}},
	}

	text := callToolText(t, "ar_by_customer", mcpserver.DispatchTool("ar_by_customer", deps), nil)
	assert.NotContains(t, text, "total_sek")

	var groups []struct {
		CustomerNumber int    `json:"customer_number"`
		CustomerName   string `json:"customer_name"`
		Count          int    `json:"count"`
		Currencies     []struct {
			Currency string  `json:"currency"`
			Total    float64 `json:"total"`
		} `json:"currencies"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &groups))

	require.Len(t, groups, 1)
	assert.Equal(t, 5, groups[0].CustomerNumber)
	assert.Equal(t, "Kund AB", groups[0].CustomerName)
	assert.Equal(t, 2, groups[0].Count)
	require.Len(t, groups[0].Currencies, 2)
	assert.Equal(t, "EUR", groups[0].Currencies[0].Currency)
	assert.InDelta(t, 2500.00, groups[0].Currencies[0].Total, 1e-9)
	assert.Equal(t, "SEK", groups[0].Currencies[1].Currency)
	assert.InDelta(t, 1200.00, groups[0].Currencies[1].Total, 1e-9)
}
