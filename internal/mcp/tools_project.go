package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerProjectTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("project_list",
		mcp.WithDescription("List all projects in Fortnox."),
	), projectListHandler(deps))

	s.AddTool(mcp.NewTool("project_transactions",
		mcp.WithDescription("All voucher rows posted to a project within an optional date range."),
		mcp.WithString("project_id",
			mcp.Description("Fortnox project number/ID."),
			mcp.Required(),
		),
		mcp.WithString("from_date",
			mcp.Description("Start date (YYYY-MM-DD). Defaults to start of current year."),
		),
		mcp.WithString("to_date",
			mcp.Description("End date (YYYY-MM-DD). Defaults to today."),
		),
	), projectTransactionsHandler(deps))

	s.AddTool(mcp.NewTool("project_profitability",
		mcp.WithDescription("Calculate project profitability by summing debit rows (cost) vs credit rows (revenue) from all posted voucher rows."),
		mcp.WithString("project_id",
			mcp.Description("Fortnox project number/ID."),
			mcp.Required(),
		),
	), projectProfitabilityHandler(deps))
}

// --- project_list ---

func projectListHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projects, err := deps.ProjectLdg.Projects(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch projects: %v", err)), nil
		}

		return jsonResult(projects)
	}
}

// --- project_transactions ---

func projectTransactionsHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectID, _ := req.GetArguments()["project_id"].(string)
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required"), nil
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

		rows, err := deps.ProjectLdg.ProjectTransactions(ctx, deps.TenantID, projectID, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch project transactions: %v", err)), nil
		}

		return jsonResult(rows)
	}
}

// --- project_profitability ---

type projectProfitabilityResult struct {
	ProjectID   string  `json:"project_id"`
	TotalDebit  float64 `json:"total_debit"`
	TotalCredit float64 `json:"total_credit"`
	NetResult   float64 `json:"net_result"`
	RowCount    int     `json:"row_count"`
}

func projectProfitabilityHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectID, _ := req.GetArguments()["project_id"].(string)
		if projectID == "" {
			return mcp.NewToolResultError("project_id is required"), nil
		}

		// Fetch all history (epoch to far future) to get the complete picture.
		from := time.Time{}
		to := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

		rows, err := deps.ProjectLdg.ProjectTransactions(ctx, deps.TenantID, projectID, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch project transactions: %v", err)), nil
		}

		var totalDebit, totalCredit int64
		for _, row := range rows {
			totalDebit += row.Debit.MinorUnits
			totalCredit += row.Credit.MinorUnits
		}

		result := projectProfitabilityResult{
			ProjectID:   projectID,
			TotalDebit:  float64(totalDebit) / 100,
			TotalCredit: float64(totalCredit) / 100,
			NetResult:   float64(totalCredit-totalDebit) / 100,
			RowCount:    len(rows),
		}

		return jsonResult(result)
	}
}
