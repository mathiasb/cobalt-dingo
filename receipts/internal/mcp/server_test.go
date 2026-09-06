package mcp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mathiasb/coo-agent/internal/api"
	internalmcp "github.com/mathiasb/coo-agent/internal/mcp"
	"github.com/mathiasb/coo-agent/internal/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fakeClient implements api.FortnoxClient for tests ---

type fakeClient struct {
	invoices         []api.Invoice
	invoiceErr       error
	vouchers         []api.Voucher
	voucherErr       error
	accounts         []validator.Account
	accountErr       error
	supplierInvoices []api.SupplierInvoice
	supplierInvErr   error
	assets           []api.Asset
	assetErr         error
	companyInfo      *api.CompanyInfo
	companyErr       error
	createdVoucher   *api.Voucher
	createErr        error
}

func (f *fakeClient) ListInvoices(_ context.Context, _ string) ([]api.Invoice, error) {
	return f.invoices, f.invoiceErr
}

func (f *fakeClient) ListVouchers(_ context.Context, _, _ time.Time) ([]api.Voucher, error) {
	return f.vouchers, f.voucherErr
}

func (f *fakeClient) ListAccounts(_ context.Context) ([]validator.Account, error) {
	return f.accounts, f.accountErr
}

func (f *fakeClient) ListSupplierInvoices(_ context.Context, _ string) ([]api.SupplierInvoice, error) {
	return f.supplierInvoices, f.supplierInvErr
}

func (f *fakeClient) ListAssets(_ context.Context) ([]api.Asset, error) {
	return f.assets, f.assetErr
}

func (f *fakeClient) GetCompanyInfo(_ context.Context) (*api.CompanyInfo, error) {
	return f.companyInfo, f.companyErr
}

func (f *fakeClient) CreateVoucher(_ context.Context, v api.Voucher) (*api.Voucher, error) {
	f.createdVoucher = &v
	if f.createErr != nil {
		return nil, f.createErr
	}
	result := v
	result.VoucherNumber = 42
	result.VoucherSeries = "A"
	return &result, nil
}

// --- helpers ---

func callTool(t *testing.T, s *internalmcp.Server, toolName string, args map[string]any) *mcplib.CallToolResult {
	t.Helper()
	result, err := s.CallTool(context.Background(), toolName, args)
	require.NoError(t, err, "tool call should not return protocol error")
	return result
}

// --- Tests ---

func TestMCPServer_New_ReturnsNonNil(t *testing.T) {
	client := &fakeClient{}
	s := internalmcp.New(client)
	assert.NotNil(t, s)
}

func TestMCPServer_Tools_AreRegistered(t *testing.T) {
	client := &fakeClient{}
	s := internalmcp.New(client)

	tools := s.ListTools()
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}

	expectedTools := []string{
		"list_invoices",
		"list_vouchers",
		"list_accounts",
		"list_supplier_invoices",
		"list_assets",
		"get_company_info",
		"create_voucher",
	}
	for _, expected := range expectedTools {
		assert.Contains(t, names, expected, "tool %q should be registered", expected)
	}
}

// --- list_invoices ---

func TestMCPServer_ListInvoices_ReturnsInvoices(t *testing.T) {
	client := &fakeClient{
		invoices: []api.Invoice{
			{DocumentNumber: "100", CustomerName: "Acme AB", Total: 12500.0, Balance: 12500.0},
			{DocumentNumber: "101", CustomerName: "Acme AB", Total: 6250.0, Balance: 0.0, Booked: true},
		},
	}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_invoices", map[string]any{})
	require.False(t, result.IsError, "should not be error")
	require.NotEmpty(t, result.Content)

	text := extractText(t, result)
	assert.Contains(t, text, "100")
	assert.Contains(t, text, "Acme AB")
	assert.Contains(t, text, "12500")
}

func TestMCPServer_ListInvoices_AcceptsFilterParam(t *testing.T) {
	client := &fakeClient{invoices: []api.Invoice{}}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_invoices", map[string]any{"filter": "unpaid"})
	assert.False(t, result.IsError)
}

func TestMCPServer_ListInvoices_ReturnsErrorOnClientFailure(t *testing.T) {
	client := &fakeClient{invoiceErr: assert.AnError}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_invoices", map[string]any{})
	assert.True(t, result.IsError)
}

// --- list_vouchers ---

func TestMCPServer_ListVouchers_ReturnsVouchers(t *testing.T) {
	client := &fakeClient{
		vouchers: []api.Voucher{
			{VoucherSeries: "A", VoucherNumber: 1, Description: "Telenor feb 2025"},
			{VoucherSeries: "A", VoucherNumber: 2, Description: "Telia feb 2025"},
		},
	}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_vouchers", map[string]any{
		"from_date": "2025-02-01",
		"to_date":   "2025-02-28",
	})
	require.False(t, result.IsError)
	text := extractText(t, result)
	assert.Contains(t, text, "Telenor")
	assert.Contains(t, text, "Telia")
}

func TestMCPServer_ListVouchers_RequiresDateParams(t *testing.T) {
	client := &fakeClient{}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_vouchers", map[string]any{})
	assert.True(t, result.IsError, "should fail without date params")
}

func TestMCPServer_ListVouchers_RejectsInvalidDateFormat(t *testing.T) {
	client := &fakeClient{}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_vouchers", map[string]any{
		"from_date": "01/02/2025",
		"to_date":   "28/02/2025",
	})
	assert.True(t, result.IsError, "should reject non-ISO dates")
}

// --- list_accounts ---

func TestMCPServer_ListAccounts_ReturnsAccounts(t *testing.T) {
	client := &fakeClient{
		accounts: []validator.Account{
			{Number: 1930, Description: "Bankkonto"},
			{Number: 3001, Description: "Försäljning tjänster"},
			{Number: 6212, Description: "Mobiltelefon"},
		},
	}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_accounts", map[string]any{})
	require.False(t, result.IsError)
	text := extractText(t, result)
	assert.Contains(t, text, "1930")
	assert.Contains(t, text, "Bankkonto")
}

// --- list_supplier_invoices ---

func TestMCPServer_ListSupplierInvoices_ReturnsInvoices(t *testing.T) {
	client := &fakeClient{
		supplierInvoices: []api.SupplierInvoice{
			{GivenNumber: "SI-1", SupplierName: "Telenor Sverige AB", Total: "1250.00", InvoiceDate: "2025-02-01"},
		},
	}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_supplier_invoices", map[string]any{})
	require.False(t, result.IsError)
	text := extractText(t, result)
	assert.Contains(t, text, "Telenor")
	assert.Contains(t, text, "SI-1")
}

// --- list_assets ---

func TestMCPServer_ListAssets_ReturnsAssets(t *testing.T) {
	client := &fakeClient{
		assets: []api.Asset{
			{Number: "INV-001", Description: "MacBook Pro M2", Status: "Active", AcquisitionValue: 25000},
			{Number: "INV-002", Description: "BMW leasing", Status: "Active", AcquisitionValue: 450000},
		},
	}
	s := internalmcp.New(client)

	result := callTool(t, s, "list_assets", map[string]any{})
	require.False(t, result.IsError)
	text := extractText(t, result)
	assert.Contains(t, text, "MacBook")
	assert.Contains(t, text, "BMW")
}

// --- get_company_info ---

func TestMCPServer_GetCompanyInfo_ReturnsInfo(t *testing.T) {
	client := &fakeClient{
		companyInfo: &api.CompanyInfo{
			CompanyName:        "Mathias Berg",
			OrganizationNumber: "820101-1234",
			City:               "Stockholm",
		},
	}
	s := internalmcp.New(client)

	result := callTool(t, s, "get_company_info", map[string]any{})
	require.False(t, result.IsError)
	text := extractText(t, result)
	assert.Contains(t, text, "Mathias Berg")
	assert.Contains(t, text, "820101-1234")
}

// --- create_voucher (WRITE – kräver confirmed=true) ---

func TestMCPServer_CreateVoucher_RequiresConfirmation(t *testing.T) {
	client := &fakeClient{}
	s := internalmcp.New(client)

	result := callTool(t, s, "create_voucher", map[string]any{
		"description":      "Telenor feb 2025",
		"transaction_date": "2025-02-28",
		"voucher_series":   "A",
		"year":             2025,
		"rows": []any{
			map[string]any{"account": 6212, "debit": 1000.0},
			map[string]any{"account": 2640, "debit": 250.0},
			map[string]any{"account": 2893, "credit": 1250.0},
		},
		// confirmed NOT set → should be blocked
	})
	assert.True(t, result.IsError, "write without confirmation must be blocked")
	text := extractText(t, result)
	assert.Contains(t, text, "bekräftelse", "error message should ask for confirmation (in Swedish)")
}

func TestMCPServer_CreateVoucher_WithConfirmation_CreatesVoucher(t *testing.T) {
	client := &fakeClient{}
	s := internalmcp.New(client)

	result := callTool(t, s, "create_voucher", map[string]any{
		"description":      "Telenor feb 2025",
		"transaction_date": "2025-02-28",
		"voucher_series":   "A",
		"year":             2025,
		"rows": []any{
			map[string]any{"account": 6212, "debit": 1000.0},
			map[string]any{"account": 2640, "debit": 250.0},
			map[string]any{"account": 2893, "credit": 1250.0},
		},
		"confirmed": true,
	})
	require.False(t, result.IsError, "confirmed create_voucher should succeed")

	require.NotNil(t, client.createdVoucher, "voucher should have been sent to API")
	assert.Equal(t, "Telenor feb 2025", client.createdVoucher.Description)
	assert.Equal(t, 3, len(client.createdVoucher.Rows))
}

func TestMCPServer_CreateVoucher_WithConfirmation_ReturnsVoucherDetails(t *testing.T) {
	client := &fakeClient{}
	s := internalmcp.New(client)

	result := callTool(t, s, "create_voucher", map[string]any{
		"description":      "Telenor feb 2025",
		"transaction_date": "2025-02-28",
		"voucher_series":   "A",
		"year":             2025,
		"rows": []any{
			map[string]any{"account": 6212, "debit": 1000.0},
			map[string]any{"account": 2893, "credit": 1000.0},
		},
		"confirmed": true,
	})
	require.False(t, result.IsError)
	text := extractText(t, result)
	// fake client sets VoucherNumber=42 and VoucherSeries="A"
	assert.Contains(t, text, "42")
	assert.Contains(t, text, "A")
}

func TestMCPServer_CreateVoucher_PropagatesAPIError(t *testing.T) {
	client := &fakeClient{createErr: assert.AnError}
	s := internalmcp.New(client)

	result := callTool(t, s, "create_voucher", map[string]any{
		"description":      "test",
		"transaction_date": "2025-02-28",
		"voucher_series":   "A",
		"year":             2025,
		"rows": []any{
			map[string]any{"account": 1930, "debit": 100.0},
			map[string]any{"account": 2893, "credit": 100.0},
		},
		"confirmed": true,
	})
	assert.True(t, result.IsError)
}

// --- helpers ---

func extractText(t *testing.T, result *mcplib.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)
	// Content may be []TextContent or similar; marshal+unmarshal to get text
	b, err := json.Marshal(result.Content[0])
	require.NoError(t, err)
	var tc struct {
		Text string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(b, &tc))
	return tc.Text
}
