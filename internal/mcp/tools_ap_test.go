package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSupplierLedger implements domain.SupplierLedger with a fixed set of
// unpaid invoices. Only UnpaidInvoices is exercised by ap_by_supplier.
type fakeSupplierLedger struct {
	invoices []domain.SupplierInvoice
}

func (f fakeSupplierLedger) UnpaidInvoices(_ context.Context, _ domain.TenantID) ([]domain.SupplierInvoice, error) {
	return f.invoices, nil
}

func (f fakeSupplierLedger) StateCounts(_ context.Context, _ domain.TenantID) ([]domain.InvoiceStateCount, error) {
	return nil, nil
}

func (f fakeSupplierLedger) UnbookedInvoices(_ context.Context, _ domain.TenantID) ([]domain.SupplierInvoice, error) {
	return nil, nil
}

func (f fakeSupplierLedger) InvoicePayments(_ context.Context, _ domain.TenantID, _ int) ([]domain.SupplierPayment, error) {
	return nil, nil
}

func (f fakeSupplierLedger) SupplierDetail(_ context.Context, _ domain.TenantID, _ int) (domain.Supplier, error) {
	return domain.Supplier{}, nil
}

// callToolText invokes a tool handler and returns the text of its single
// text-content result.
func callToolText(t *testing.T, name string, handler server.ToolHandlerFunc, args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	res, err := handler(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.False(t, res.IsError, "tool returned error result")
	require.Len(t, res.Content, 1)

	text, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok, "result content is not text")
	return text.Text
}

// TestAPBySupplierReturnsPerCurrencyTotals verifies that ap_by_supplier never
// collapses amounts across currencies into a single figure: a supplier with
// EUR 1000.00 and SEK 500.00 unpaid must report one total per currency, and
// the result must not carry a total_sek field — no FX conversion happens, so
// a single SEK total would be fabricated.
func TestAPBySupplierReturnsPerCurrencyTotals(t *testing.T) {
	deps := mcpserver.Deps{
		TenantID: domain.TenantID("test"),
		SupplierLdg: fakeSupplierLedger{invoices: []domain.SupplierInvoice{
			{InvoiceNumber: 1, SupplierNumber: 10, SupplierName: "Mixed AB", Amount: domain.Money{MinorUnits: 100000, Currency: "EUR"}, DueDate: "2026-01-01"},
			{InvoiceNumber: 2, SupplierNumber: 10, SupplierName: "Mixed AB", Amount: domain.Money{MinorUnits: 50000, Currency: "SEK"}, DueDate: "2026-01-02"},
			{InvoiceNumber: 3, SupplierNumber: 20, SupplierName: "Svensk AB", Amount: domain.Money{MinorUnits: 70000, Currency: "SEK"}, DueDate: "2026-01-03"},
		}},
	}

	text := callToolText(t, "ap_by_supplier", mcpserver.DispatchTool("ap_by_supplier", deps), nil)
	assert.NotContains(t, text, "total_sek")

	var groups []struct {
		SupplierNumber int    `json:"supplier_number"`
		SupplierName   string `json:"supplier_name"`
		Count          int    `json:"count"`
		Currencies     []struct {
			Currency string  `json:"currency"`
			Total    float64 `json:"total"`
		} `json:"currencies"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &groups))

	// Sorting uses each group's first-currency total, so Mixed AB (EUR 1000.00)
	// sorts ahead of Svensk AB (SEK 700.00).
	require.Len(t, groups, 2)

	assert.Equal(t, 10, groups[0].SupplierNumber)
	assert.Equal(t, "Mixed AB", groups[0].SupplierName)
	assert.Equal(t, 2, groups[0].Count)
	require.Len(t, groups[0].Currencies, 2)
	assert.Equal(t, "EUR", groups[0].Currencies[0].Currency)
	assert.InDelta(t, 1000.00, groups[0].Currencies[0].Total, 1e-9)
	assert.Equal(t, "SEK", groups[0].Currencies[1].Currency)
	assert.InDelta(t, 500.00, groups[0].Currencies[1].Total, 1e-9)

	assert.Equal(t, 20, groups[1].SupplierNumber)
	assert.Equal(t, "Svensk AB", groups[1].SupplierName)
	assert.Equal(t, 1, groups[1].Count)
	require.Len(t, groups[1].Currencies, 1)
	assert.Equal(t, "SEK", groups[1].Currencies[0].Currency)
	assert.InDelta(t, 700.00, groups[1].Currencies[0].Total, 1e-9)
}
