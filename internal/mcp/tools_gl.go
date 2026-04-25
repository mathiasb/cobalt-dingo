package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

func registerGLTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("gl_chart_of_accounts",
		mcp.WithDescription("Return all GL accounts for a financial year (defaults to the latest year)."),
		mcp.WithNumber("year_id",
			mcp.Description("Fortnox financial year ID. Defaults to the latest year."),
		),
	), glChartOfAccountsHandler(deps))

	s.AddTool(mcp.NewTool("gl_account_balance",
		mcp.WithDescription("Period balances for a range of GL accounts."),
		mcp.WithNumber("account_from",
			mcp.Description("First account number in the range."),
			mcp.Required(),
		),
		mcp.WithNumber("account_to",
			mcp.Description("Last account number in the range."),
			mcp.Required(),
		),
		mcp.WithNumber("year_id",
			mcp.Description("Fortnox financial year ID. Defaults to the latest year."),
		),
	), glAccountBalanceHandler(deps))

	s.AddTool(mcp.NewTool("gl_account_activity",
		mcp.WithDescription("All voucher rows posted to a specific GL account within a date range."),
		mcp.WithNumber("account_num",
			mcp.Description("GL account number."),
			mcp.Required(),
		),
		mcp.WithString("from_date",
			mcp.Description("Start date (YYYY-MM-DD)."),
			mcp.Required(),
		),
		mcp.WithString("to_date",
			mcp.Description("End date (YYYY-MM-DD)."),
			mcp.Required(),
		),
		mcp.WithNumber("year_id",
			mcp.Description("Fortnox financial year ID. Defaults to the latest year."),
		),
	), glAccountActivityHandler(deps))

	s.AddTool(mcp.NewTool("gl_vouchers",
		mcp.WithDescription("All vouchers (journal entries) posted within a date range."),
		mcp.WithString("from_date",
			mcp.Description("Start date (YYYY-MM-DD)."),
			mcp.Required(),
		),
		mcp.WithString("to_date",
			mcp.Description("End date (YYYY-MM-DD)."),
			mcp.Required(),
		),
		mcp.WithNumber("year_id",
			mcp.Description("Fortnox financial year ID. Defaults to the latest year."),
		),
	), glVouchersHandler(deps))

	s.AddTool(mcp.NewTool("gl_voucher_detail",
		mcp.WithDescription("Full detail for a single voucher identified by series and number."),
		mcp.WithString("series",
			mcp.Description("Voucher series (e.g. A, B, SIE)."),
			mcp.Required(),
		),
		mcp.WithNumber("number",
			mcp.Description("Voucher number within the series."),
			mcp.Required(),
		),
	), glVoucherDetailHandler(deps))

	s.AddTool(mcp.NewTool("gl_predefined_accounts",
		mcp.WithDescription("Return the system-defined account mappings (e.g. VAT, retained earnings)."),
	), glPredefinedAccountsHandler(deps))

	s.AddTool(mcp.NewTool("gl_financial_years",
		mcp.WithDescription("List all financial years configured in Fortnox."),
	), glFinancialYearsHandler(deps))
}

// latestYearID returns the ID of the financial year with the highest To date.
// Returns 0 if years is empty.
func latestYearID(years []domain.FinancialYear) int {
	if len(years) == 0 {
		return 0
	}
	best := years[0]
	for _, y := range years[1:] {
		if y.To.After(best.To) {
			best = y
		}
	}
	return best.ID
}

// resolveYearID returns the year_id from the request or falls back to the latest.
func resolveYearID(ctx context.Context, req mcp.CallToolRequest, deps Deps) (int, error) {
	yearID := req.GetInt("year_id", 0)
	if yearID != 0 {
		return yearID, nil
	}
	years, err := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
	if err != nil {
		return 0, fmt.Errorf("fetch financial years: %w", err)
	}
	return latestYearID(years), nil
}

// --- gl_chart_of_accounts ---

func glChartOfAccountsHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		yearID, err := resolveYearID(ctx, req, deps)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		accounts, err := deps.GeneralLdg.ChartOfAccounts(ctx, deps.TenantID, yearID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch chart of accounts: %v", err)), nil
		}

		return jsonResult(accounts)
	}
}

// --- gl_account_balance ---

func glAccountBalanceHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		accountFrom, err := req.RequireInt("account_from")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("account_from required: %v", err)), nil
		}

		accountTo, err := req.RequireInt("account_to")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("account_to required: %v", err)), nil
		}

		yearID, err := resolveYearID(ctx, req, deps)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		balances, err := deps.GeneralLdg.AccountBalances(ctx, deps.TenantID, yearID, accountFrom, accountTo)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch account balances: %v", err)), nil
		}

		return jsonResult(balances)
	}
}

// --- gl_account_activity ---

func glAccountActivityHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		accountNum, err := req.RequireInt("account_num")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("account_num required: %v", err)), nil
		}

		fromStr, _ := req.GetArguments()["from_date"].(string)
		toStr, _ := req.GetArguments()["to_date"].(string)

		if fromStr == "" {
			return mcp.NewToolResultError("from_date is required"), nil
		}
		if toStr == "" {
			return mcp.NewToolResultError("to_date is required"), nil
		}

		from, err := time.Parse("2006-01-02", fromStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid from_date: %v", err)), nil
		}
		to, err := time.Parse("2006-01-02", toStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid to_date: %v", err)), nil
		}

		yearID, err := resolveYearID(ctx, req, deps)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		rows, err := deps.GeneralLdg.AccountActivity(ctx, deps.TenantID, yearID, accountNum, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch account activity: %v", err)), nil
		}

		return jsonResult(rows)
	}
}

// --- gl_vouchers ---

func glVouchersHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fromStr, _ := req.GetArguments()["from_date"].(string)
		toStr, _ := req.GetArguments()["to_date"].(string)

		if fromStr == "" {
			return mcp.NewToolResultError("from_date is required"), nil
		}
		if toStr == "" {
			return mcp.NewToolResultError("to_date is required"), nil
		}

		from, err := time.Parse("2006-01-02", fromStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid from_date: %v", err)), nil
		}
		to, err := time.Parse("2006-01-02", toStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid to_date: %v", err)), nil
		}

		yearID, err := resolveYearID(ctx, req, deps)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		vouchers, err := deps.GeneralLdg.Vouchers(ctx, deps.TenantID, yearID, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch vouchers: %v", err)), nil
		}

		return jsonResult(vouchers)
	}
}

// --- gl_voucher_detail ---

func glVoucherDetailHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		series, _ := req.GetArguments()["series"].(string)
		if series == "" {
			return mcp.NewToolResultError("series is required"), nil
		}

		number, err := req.RequireInt("number")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("number required: %v", err)), nil
		}

		voucher, err := deps.GeneralLdg.VoucherDetail(ctx, deps.TenantID, series, number)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch voucher detail: %v", err)), nil
		}

		return jsonResult(voucher)
	}
}

// --- gl_predefined_accounts ---

func glPredefinedAccountsHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		accounts, err := deps.GeneralLdg.PredefinedAccounts(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch predefined accounts: %v", err)), nil
		}

		return jsonResult(accounts)
	}
}

// --- gl_financial_years ---

func glFinancialYearsHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		years, err := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch financial years: %v", err)), nil
		}

		return jsonResult(years)
	}
}
