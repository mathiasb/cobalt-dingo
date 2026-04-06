// Package mcp implements the Model Context Protocol server for coo-agent.
//
// Security levels (from CLAUDE.md / .claude/rules/security.md):
//
//	Green  – read operations, called automatically, no confirmation needed.
//	Yellow – write operations, require explicit "ja" (confirmed=true) from the user.
//	Red    – always blocked, not registered as tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mathiasb/coo-agent/internal/api"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go MCPServer and holds a reference to the Fortnox client.
type Server struct {
	mcp    *mcpserver.MCPServer
	client api.FortnoxClient
}

// New creates a new MCP server with all Fortnox tools registered.
func New(client api.FortnoxClient) *Server {
	s := &Server{
		client: client,
		mcp: mcpserver.NewMCPServer(
			"coo-agent",
			"0.1.0",
			mcpserver.WithToolCapabilities(false),
		),
	}
	s.registerTools()
	return s
}

// ServeStdio runs the MCP server over stdin/stdout (for Claude Desktop integration).
func (s *Server) ServeStdio(ctx context.Context) error {
	stdio := mcpserver.NewStdioServer(s.mcp)
	return stdio.Listen(ctx, nil, nil)
}

// ListTools returns all registered tools (used in tests).
func (s *Server) ListTools() []mcplib.Tool {
	result := s.mcp.HandleMessage(context.Background(), marshalRequest("tools/list", map[string]any{}))
	resp, ok := result.(mcplib.JSONRPCResponse)
	if !ok {
		return nil
	}
	lr, ok := resp.Result.(mcplib.ListToolsResult)
	if !ok {
		return nil
	}
	return lr.Tools
}

// CallTool invokes a registered tool by name with the given arguments.
// It returns the CallToolResult. Tool-level errors are reflected in result.IsError.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]any) (*mcplib.CallToolResult, error) {
	msg := marshalRequest("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	result := s.mcp.HandleMessage(ctx, msg)
	resp, ok := result.(mcplib.JSONRPCResponse)
	if !ok {
		// Protocol-level error (e.g. tool not found)
		return nil, fmt.Errorf("mcp: unexpected response type for tool %q: %T", name, result)
	}
	ctr, ok := resp.Result.(*mcplib.CallToolResult)
	if !ok {
		return nil, fmt.Errorf("mcp: unexpected result type for tool %q: %T", name, resp.Result)
	}
	return ctr, nil
}

// --- tool registration ---

func (s *Server) registerTools() {
	// --- GREEN: read tools ---
	s.mcp.AddTool(
		mcplib.NewTool("list_invoices",
			mcplib.WithDescription("Lista kundfakturor från Fortnox. Returnerar dokumentnummer, kundnamn, datum, belopp och status."),
			mcplib.WithString("filter",
				mcplib.Description("Filtrera: unpaid, unpaidoverdue, fullypaid, cancelled (lämna tomt för alla)"),
			),
		),
		s.handleListInvoices,
	)

	s.mcp.AddTool(
		mcplib.NewTool("list_vouchers",
			mcplib.WithDescription("Lista verifikationer för ett datumintervall. Returnerar serie, nummer och beskrivning."),
			mcplib.WithString("from_date",
				mcplib.Description("Startdatum (YYYY-MM-DD)"),
				mcplib.Required(),
			),
			mcplib.WithString("to_date",
				mcplib.Description("Slutdatum (YYYY-MM-DD)"),
				mcplib.Required(),
			),
		),
		s.handleListVouchers,
	)

	s.mcp.AddTool(
		mcplib.NewTool("list_accounts",
			mcplib.WithDescription("Lista BAS-kontoplanen från Fortnox. Returnerar kontonummer, namn och momskod."),
		),
		s.handleListAccounts,
	)

	s.mcp.AddTool(
		mcplib.NewTool("list_supplier_invoices",
			mcplib.WithDescription("Lista leverantörsfakturor från Fortnox. Returnerar fakturanummer, leverantör, datum och belopp."),
			mcplib.WithString("filter",
				mcplib.Description("Filtrera: unpaid, unpaidoverdue, fullypaid, cancelled (lämna tomt för alla)"),
			),
		),
		s.handleListSupplierInvoices,
	)

	s.mcp.AddTool(
		mcplib.NewTool("list_assets",
			mcplib.WithDescription("Lista anläggningstillgångar (inventarier, fordon) från Fortnox."),
		),
		s.handleListAssets,
	)

	s.mcp.AddTool(
		mcplib.NewTool("get_company_info",
			mcplib.WithDescription("Hämta grundläggande företagsinformation (namn, organisationsnummer, adress)."),
		),
		s.handleGetCompanyInfo,
	)

	// --- YELLOW: write tools (require confirmed=true) ---
	s.mcp.AddTool(
		mcplib.NewTool("create_voucher",
			mcplib.WithDescription("Skapa en ny verifikation i Fortnox. KRÄVER explicit bekräftelse från användaren (confirmed=true). Fråga alltid användaren innan du anropar detta verktyg."),
			mcplib.WithString("description",
				mcplib.Description("Verifikationstext (leverantör + period)"),
				mcplib.Required(),
			),
			mcplib.WithString("transaction_date",
				mcplib.Description("Transaktionsdatum (YYYY-MM-DD)"),
				mcplib.Required(),
			),
			mcplib.WithString("voucher_series",
				mcplib.Description("Verifikationsserie, t.ex. A"),
				mcplib.Required(),
			),
			mcplib.WithNumber("year",
				mcplib.Description("Räkenskapsår-ID från Fortnox (heltal)"),
				mcplib.Required(),
			),
			mcplib.WithArray("rows",
				mcplib.Description("Verifikationsrader. Varje rad: {account, debit, credit, description}"),
				mcplib.Required(),
			),
			mcplib.WithBoolean("confirmed",
				mcplib.Description("Sätt till true för att bekräfta att användaren har godkänt skapandet."),
			),
		),
		s.handleCreateVoucher,
	)
}

// --- handlers ---

func (s *Server) handleListInvoices(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	filter, _ := args["filter"].(string)

	invoices, err := s.client.ListInvoices(ctx, filter)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Kunde inte hämta fakturor: %v", err)), nil
	}

	b, err := json.MarshalIndent(invoices, "", "  ")
	if err != nil {
		return mcplib.NewToolResultError("Kunde inte serialisera fakturor"), nil
	}
	return mcplib.NewToolResultText(string(b)), nil
}

func (s *Server) handleListVouchers(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	fromStr, _ := args["from_date"].(string)
	toStr, _ := args["to_date"].(string)

	if fromStr == "" || toStr == "" {
		return mcplib.NewToolResultError("from_date och to_date är obligatoriska"), nil
	}
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Ogiltigt from_date-format (YYYY-MM-DD krävs): %v", err)), nil
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Ogiltigt to_date-format (YYYY-MM-DD krävs): %v", err)), nil
	}

	vouchers, err := s.client.ListVouchers(ctx, from, to)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Kunde inte hämta verifikationer: %v", err)), nil
	}

	b, err := json.MarshalIndent(vouchers, "", "  ")
	if err != nil {
		return mcplib.NewToolResultError("Kunde inte serialisera verifikationer"), nil
	}
	return mcplib.NewToolResultText(string(b)), nil
}

func (s *Server) handleListAccounts(ctx context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	accounts, err := s.client.ListAccounts(ctx)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Kunde inte hämta konton: %v", err)), nil
	}

	b, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return mcplib.NewToolResultError("Kunde inte serialisera konton"), nil
	}
	return mcplib.NewToolResultText(string(b)), nil
}

func (s *Server) handleListSupplierInvoices(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	filter, _ := args["filter"].(string)

	invoices, err := s.client.ListSupplierInvoices(ctx, filter)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Kunde inte hämta leverantörsfakturor: %v", err)), nil
	}

	b, err := json.MarshalIndent(invoices, "", "  ")
	if err != nil {
		return mcplib.NewToolResultError("Kunde inte serialisera leverantörsfakturor"), nil
	}
	return mcplib.NewToolResultText(string(b)), nil
}

func (s *Server) handleListAssets(ctx context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	assets, err := s.client.ListAssets(ctx)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Kunde inte hämta anläggningstillgångar: %v", err)), nil
	}

	b, err := json.MarshalIndent(assets, "", "  ")
	if err != nil {
		return mcplib.NewToolResultError("Kunde inte serialisera tillgångar"), nil
	}
	return mcplib.NewToolResultText(string(b)), nil
}

func (s *Server) handleGetCompanyInfo(ctx context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	info, err := s.client.GetCompanyInfo(ctx)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Kunde inte hämta företagsinformation: %v", err)), nil
	}

	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return mcplib.NewToolResultError("Kunde inte serialisera företagsinformation"), nil
	}
	return mcplib.NewToolResultText(string(b)), nil
}

func (s *Server) handleCreateVoucher(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()

	// Security gate: require explicit confirmation.
	confirmed, _ := args["confirmed"].(bool)
	if !confirmed {
		return mcplib.NewToolResultError(
			"Skrivoperationen kräver bekräftelse från användaren. " +
				"Fråga användaren om de vill skapa verifikationen och anropa verktyget igen med confirmed=true när de svarar ja.",
		), nil
	}

	description, _ := args["description"].(string)
	transactionDate, _ := args["transaction_date"].(string)
	voucherSeries, _ := args["voucher_series"].(string)
	yearFloat, _ := args["year"].(float64)
	year := int(yearFloat)

	rawRows, _ := args["rows"].([]any)
	rows := make([]api.VoucherRow, 0, len(rawRows))
	for _, raw := range rawRows {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		row := api.VoucherRow{}
		if v, ok := m["account"].(float64); ok {
			row.Account = int(v)
		}
		if v, ok := m["debit"].(float64); ok {
			row.Debit = v
		}
		if v, ok := m["credit"].(float64); ok {
			row.Credit = v
		}
		if v, ok := m["description"].(string); ok {
			row.Description = v
		}
		rows = append(rows, row)
	}

	voucher := api.Voucher{
		Description:   description,
		VoucherDate:   transactionDate,
		VoucherSeries: voucherSeries,
		Year:          year,
		Rows:          rows,
	}

	created, err := s.client.CreateVoucher(ctx, voucher)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Kunde inte skapa verifikation: %v", err)), nil
	}

	b, err := json.MarshalIndent(created, "", "  ")
	if err != nil {
		return mcplib.NewToolResultError("Verifikation skapad men kunde inte serialisera svaret"), nil
	}
	return mcplib.NewToolResultText(string(b)), nil
}

// --- internal helpers ---

func marshalRequest(method string, params map[string]any) json.RawMessage {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	b, _ := json.Marshal(msg)
	return b
}
