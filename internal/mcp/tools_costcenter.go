package mcp

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/analyst"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

func registerCostCtrTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("costcenter_list",
		mcp.WithDescription("List all cost centers defined in Fortnox."),
	), costCenterListHandler(deps))

	s.AddTool(mcp.NewTool("costcenter_transactions",
		mcp.WithDescription("All voucher rows posted to a cost center within an optional date range."),
		mcp.WithString("code",
			mcp.Description("Cost center code."),
			mcp.Required(),
		),
		mcp.WithString("from_date",
			mcp.Description("Start date (YYYY-MM-DD). Defaults to start of current year."),
		),
		mcp.WithString("to_date",
			mcp.Description("End date (YYYY-MM-DD). Defaults to today."),
		),
	), costCenterTransactionsHandler(deps))

	s.AddTool(mcp.NewTool("costcenter_analysis",
		mcp.WithDescription("Group all cost center transactions by account number, showing debit, credit and net for each account."),
		mcp.WithString("code",
			mcp.Description("Cost center code."),
			mcp.Required(),
		),
	), costCenterAnalysisHandler(deps))
}

// --- costcenter_list ---

func costCenterListHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		centers, err := deps.CostCtrLdg.CostCenters(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch cost centers: %v", err)), nil
		}

		return jsonResult(centers)
	}
}

// --- costcenter_transactions ---

func costCenterTransactionsHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		code, _ := req.GetArguments()["code"].(string)
		if code == "" {
			return mcp.NewToolResultError("code is required"), nil
		}

		now := time.Now()
		from := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		to := now

		if fromStr, ok := req.GetArguments()["from_date"].(string); ok && fromStr != "" {
			t, err := time.Parse("2006-01-02", fromStr)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid from_date: %v", err)), nil
			}
			from = t
		}

		if toStr, ok := req.GetArguments()["to_date"].(string); ok && toStr != "" {
			t, err := time.Parse("2006-01-02", toStr)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid to_date: %v", err)), nil
			}
			to = t
		}

		rows, err := deps.CostCtrLdg.CostCenterTransactions(ctx, deps.TenantID, code, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch cost center transactions: %v", err)), nil
		}

		return jsonResult(rows)
	}
}

// --- costcenter_analysis ---

type accountAnalysisRow struct {
	AccountNumber int     `json:"account_number"`
	Description   string  `json:"description"`
	TotalDebit    float64 `json:"total_debit"`
	TotalCredit   float64 `json:"total_credit"`
	Net           float64 `json:"net"`
	RowCount      int     `json:"row_count"`
}

func costCenterAnalysisHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		code, _ := req.GetArguments()["code"].(string)
		if code == "" {
			return mcp.NewToolResultError("code is required"), nil
		}

		// Fetch all time for full picture.
		from := time.Time{}
		to := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

		rows, err := deps.CostCtrLdg.CostCenterTransactions(ctx, deps.TenantID, code, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch cost center transactions: %v", err)), nil
		}

		grouped := analyst.GroupBy(rows, func(r domain.VoucherRow) int {
			return r.Account
		})

		var analysis []accountAnalysisRow
		for _, acct := range analyst.OrderedKeys(rows, func(r domain.VoucherRow) int {
			return r.Account
		}) {
			grp := grouped[acct]
			var debit, credit int64
			desc := ""
			for _, r := range grp {
				debit += r.Debit.MinorUnits
				credit += r.Credit.MinorUnits
				if desc == "" {
					desc = r.Description
				}
			}
			analysis = append(analysis, accountAnalysisRow{
				AccountNumber: acct,
				Description:   desc,
				TotalDebit:    float64(debit) / 100,
				TotalCredit:   float64(credit) / 100,
				Net:           float64(credit-debit) / 100,
				RowCount:      len(grp),
			})
		}

		sort.Slice(analysis, func(i, j int) bool {
			return analysis[i].AccountNumber < analysis[j].AccountNumber
		})

		return jsonResult(analysis)
	}
}
