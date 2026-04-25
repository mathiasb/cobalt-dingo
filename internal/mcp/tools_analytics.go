package mcp

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

func registerAnalyticsTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("cash_flow_forecast",
		mcp.WithDescription("Forecast cash flow over a horizon: inflows from unpaid AR, outflows from unpaid AP, both filtered to invoices due within the horizon."),
		mcp.WithNumber("days_ahead",
			mcp.Description("Number of days to look ahead (default 30)."),
		),
	), cashFlowForecastHandler(deps))

	s.AddTool(mcp.NewTool("expense_analysis",
		mcp.WithDescription("Group debit amounts for BAS expense accounts (4000-7999) by account for a date range, sorted by total descending."),
		mcp.WithString("from_date",
			mcp.Description("Start of period, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithString("to_date",
			mcp.Description("End of period, YYYY-MM-DD."),
			mcp.Required(),
		),
	), expenseAnalysisHandler(deps))

	s.AddTool(mcp.NewTool("period_comparison",
		mcp.WithDescription("Compare revenue and COGS side-by-side for two date ranges."),
		mcp.WithString("period1_from",
			mcp.Description("Period 1 start, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithString("period1_to",
			mcp.Description("Period 1 end, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithString("period2_from",
			mcp.Description("Period 2 start, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithString("period2_to",
			mcp.Description("Period 2 end, YYYY-MM-DD."),
			mcp.Required(),
		),
	), periodComparisonHandler(deps))

	s.AddTool(mcp.NewTool("yearly_comparison",
		mcp.WithDescription("Compare account balances (1000-9999) for two financial years side-by-side."),
		mcp.WithNumber("year1",
			mcp.Description("First financial year ID."),
			mcp.Required(),
		),
		mcp.WithNumber("year2",
			mcp.Description("Second financial year ID."),
			mcp.Required(),
		),
	), yearlyComparisonHandler(deps))

	s.AddTool(mcp.NewTool("gross_margin_trend",
		mcp.WithDescription("Monthly gross margin trend: revenue, COGS and margin % for each of the last N months."),
		mcp.WithNumber("months",
			mcp.Description("Number of months to include (default 12)."),
		),
	), grossMarginTrendHandler(deps))

	s.AddTool(mcp.NewTool("top_customers",
		mcp.WithDescription("Rank customers by unpaid AR invoice totals for a date range."),
		mcp.WithString("from_date",
			mcp.Description("Start of period, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithString("to_date",
			mcp.Description("End of period, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of customers to return (default 10)."),
		),
	), topCustomersHandler(deps))

	s.AddTool(mcp.NewTool("top_suppliers",
		mcp.WithDescription("Rank suppliers by unpaid AP invoice totals for a date range."),
		mcp.WithString("from_date",
			mcp.Description("Start of period, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithString("to_date",
			mcp.Description("End of period, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of suppliers to return (default 10)."),
		),
	), topSuppliersHandler(deps))

	s.AddTool(mcp.NewTool("sales_vs_purchases",
		mcp.WithDescription("Compare revenue (3000-3999 credit) and COGS (4000-4999 debit) for a period, returning both totals and the net."),
		mcp.WithString("from_date",
			mcp.Description("Start of period, YYYY-MM-DD."),
			mcp.Required(),
		),
		mcp.WithString("to_date",
			mcp.Description("End of period, YYYY-MM-DD."),
			mcp.Required(),
		),
	), salesVsPurchasesHandler(deps))

	s.AddTool(mcp.NewTool("company_info",
		mcp.WithDescription("Return the company profile from the ERP."),
	), companyInfoHandler(deps))
}

// --- helpers ---

type metrics struct {
	Revenue float64 `json:"revenue"`
	COGS    float64 `json:"cogs"`
}

// periodMetrics fetches vouchers for the given financial year and date window,
// then sums 3000-3999 credit rows as revenue and 4000-4999 debit rows as COGS.
func periodMetrics(ctx context.Context, deps Deps, yearID int, from, to time.Time) (metrics, error) {
	vouchers, err := deps.GeneralLdg.Vouchers(ctx, deps.TenantID, yearID, from, to)
	if err != nil {
		return metrics{}, fmt.Errorf("fetch vouchers: %w", err)
	}

	var m metrics
	for _, v := range vouchers {
		for _, row := range v.Rows {
			if row.Account >= 3000 && row.Account <= 3999 {
				m.Revenue += row.Credit.Float()
			}
			if row.Account >= 4000 && row.Account <= 4999 {
				m.COGS += row.Debit.Float()
			}
		}
	}
	return m, nil
}

const dateLayout = "2006-01-02"

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return t, nil
}

// --- cash_flow_forecast ---

type cashFlowForecastResult struct {
	InflowsTotal  float64 `json:"inflows_total"`
	InflowsCount  int     `json:"inflows_count"`
	OutflowsTotal float64 `json:"outflows_total"`
	OutflowsCount int     `json:"outflows_count"`
	NetFlow       float64 `json:"net_flow"`
	HorizonDays   int     `json:"horizon_days"`
}

func cashFlowForecastHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		days := req.GetInt("days_ahead", 30)
		horizon := time.Now().AddDate(0, 0, days)

		apInvoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch AP invoices: %v", err)), nil
		}

		arInvoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch AR invoices: %v", err)), nil
		}

		var outflows float64
		var outflowCount int
		for _, inv := range apInvoices {
			due, err := time.Parse(dateLayout, inv.DueDate)
			if err != nil {
				continue
			}
			if !due.After(horizon) {
				outflows += inv.Amount.Float()
				outflowCount++
			}
		}

		var inflows float64
		var inflowCount int
		for _, inv := range arInvoices {
			due, err := time.Parse(dateLayout, inv.DueDate)
			if err != nil {
				continue
			}
			if !due.After(horizon) {
				inflows += inv.Balance.Float()
				inflowCount++
			}
		}

		return jsonResult(cashFlowForecastResult{
			InflowsTotal:  inflows,
			InflowsCount:  inflowCount,
			OutflowsTotal: outflows,
			OutflowsCount: outflowCount,
			NetFlow:       inflows - outflows,
			HorizonDays:   days,
		})
	}
}

// --- expense_analysis ---

type expenseRow struct {
	Account     int     `json:"account"`
	Total       float64 `json:"total"`
}

func expenseAnalysisHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		fromStr, _ := args["from_date"].(string)
		toStr, _ := args["to_date"].(string)

		from, err := parseDate(fromStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("from_date: %v", err)), nil
		}
		to, err := parseDate(toStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("to_date: %v", err)), nil
		}

		years, err := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch financial years: %v", err)), nil
		}
		yearID := latestYearID(years)

		vouchers, err := deps.GeneralLdg.Vouchers(ctx, deps.TenantID, yearID, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch vouchers: %v", err)), nil
		}

		totals := map[int]float64{}
		for _, v := range vouchers {
			for _, row := range v.Rows {
				if row.Account >= 4000 && row.Account <= 7999 {
					totals[row.Account] += row.Debit.Float()
				}
			}
		}

		rows := make([]expenseRow, 0, len(totals))
		for acct, total := range totals {
			rows = append(rows, expenseRow{Account: acct, Total: total})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Total > rows[j].Total
		})

		return jsonResult(rows)
	}
}

// --- period_comparison ---

type periodComparisonResult struct {
	Period1 periodComparisonEntry `json:"period1"`
	Period2 periodComparisonEntry `json:"period2"`
}

type periodComparisonEntry struct {
	From    string  `json:"from"`
	To      string  `json:"to"`
	Revenue float64 `json:"revenue"`
	COGS    float64 `json:"cogs"`
}

func periodComparisonHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		p1FromStr, _ := args["period1_from"].(string)
		p1ToStr, _ := args["period1_to"].(string)
		p2FromStr, _ := args["period2_from"].(string)
		p2ToStr, _ := args["period2_to"].(string)

		p1From, err := parseDate(p1FromStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period1_from: %v", err)), nil
		}
		p1To, err := parseDate(p1ToStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period1_to: %v", err)), nil
		}
		p2From, err := parseDate(p2FromStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period2_from: %v", err)), nil
		}
		p2To, err := parseDate(p2ToStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period2_to: %v", err)), nil
		}

		years, err := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch financial years: %v", err)), nil
		}
		yearID := latestYearID(years)

		m1, err := periodMetrics(ctx, deps, yearID, p1From, p1To)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period1 metrics: %v", err)), nil
		}
		m2, err := periodMetrics(ctx, deps, yearID, p2From, p2To)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period2 metrics: %v", err)), nil
		}

		return jsonResult(periodComparisonResult{
			Period1: periodComparisonEntry{From: p1FromStr, To: p1ToStr, Revenue: m1.Revenue, COGS: m1.COGS},
			Period2: periodComparisonEntry{From: p2FromStr, To: p2ToStr, Revenue: m2.Revenue, COGS: m2.COGS},
		})
	}
}

// --- yearly_comparison ---

type yearlyComparisonResult struct {
	Year1     int              `json:"year1"`
	Year2     int              `json:"year2"`
	Balances1 []domain.AccountBalance `json:"balances_year1"`
	Balances2 []domain.AccountBalance `json:"balances_year2"`
}

func yearlyComparisonHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		year1 := req.GetInt("year1", 0)
		year2 := req.GetInt("year2", 0)
		if year1 == 0 || year2 == 0 {
			return mcp.NewToolResultError("year1 and year2 are required"), nil
		}

		b1, err := deps.GeneralLdg.AccountBalances(ctx, deps.TenantID, year1, 1000, 9999)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch balances year1: %v", err)), nil
		}
		b2, err := deps.GeneralLdg.AccountBalances(ctx, deps.TenantID, year2, 1000, 9999)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch balances year2: %v", err)), nil
		}

		return jsonResult(yearlyComparisonResult{
			Year1:     year1,
			Year2:     year2,
			Balances1: b1,
			Balances2: b2,
		})
	}
}

// --- gross_margin_trend ---

type grossMarginMonth struct {
	Month   string  `json:"month"`
	Revenue float64 `json:"revenue"`
	COGS    float64 `json:"cogs"`
	Margin  float64 `json:"margin_pct"`
}

func grossMarginTrendHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		months := req.GetInt("months", 12)
		if months <= 0 {
			months = 12
		}

		years, err := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch financial years: %v", err)), nil
		}
		yearID := latestYearID(years)

		now := time.Now()
		result := make([]grossMarginMonth, 0, months)

		for i := months - 1; i >= 0; i-- {
			// First day of month i months ago
			monthStart := time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, time.UTC)
			monthEnd := monthStart.AddDate(0, 1, -1)

			m, err := periodMetrics(ctx, deps, yearID, monthStart, monthEnd)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("metrics for %s: %v", monthStart.Format("2006-01"), err)), nil
			}

			var margin float64
			if m.Revenue != 0 {
				margin = (m.Revenue - m.COGS) / m.Revenue * 100
			}

			result = append(result, grossMarginMonth{
				Month:   monthStart.Format("2006-01"),
				Revenue: m.Revenue,
				COGS:    m.COGS,
				Margin:  margin,
			})
		}

		return jsonResult(result)
	}
}

// --- top_customers ---

type customerRank struct {
	CustomerNumber int     `json:"customer_number"`
	CustomerName   string  `json:"customer_name"`
	Count          int     `json:"count"`
	Total          float64 `json:"total"`
}

func topCustomersHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		fromStr, _ := args["from_date"].(string)
		toStr, _ := args["to_date"].(string)
		limit := req.GetInt("limit", 10)

		from, err := parseDate(fromStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("from_date: %v", err)), nil
		}
		to, err := parseDate(toStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("to_date: %v", err)), nil
		}

		invoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch AR invoices: %v", err)), nil
		}

		// Filter by date range using InvoiceDate
		totals := map[int]float64{}
		counts := map[int]int{}
		names := map[int]string{}
		for _, inv := range invoices {
			invDate, err := time.Parse(dateLayout, inv.InvoiceDate)
			if err != nil {
				continue
			}
			if (invDate.Equal(from) || invDate.After(from)) && (invDate.Equal(to) || invDate.Before(to)) {
				totals[inv.CustomerNumber] += inv.Amount.Float()
				counts[inv.CustomerNumber]++
				names[inv.CustomerNumber] = inv.CustomerName
			}
		}

		ranks := make([]customerRank, 0, len(totals))
		for num, total := range totals {
			ranks = append(ranks, customerRank{
				CustomerNumber: num,
				CustomerName:   names[num],
				Count:          counts[num],
				Total:          total,
			})
		}
		sort.Slice(ranks, func(i, j int) bool {
			return ranks[i].Total > ranks[j].Total
		})

		if limit > 0 && limit < len(ranks) {
			ranks = ranks[:limit]
		}

		return jsonResult(ranks)
	}
}

// --- top_suppliers ---

type supplierRank struct {
	SupplierNumber int     `json:"supplier_number"`
	SupplierName   string  `json:"supplier_name"`
	Count          int     `json:"count"`
	Total          float64 `json:"total"`
}

func topSuppliersHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		fromStr, _ := args["from_date"].(string)
		toStr, _ := args["to_date"].(string)
		limit := req.GetInt("limit", 10)

		from, err := parseDate(fromStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("from_date: %v", err)), nil
		}
		to, err := parseDate(toStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("to_date: %v", err)), nil
		}

		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch AP invoices: %v", err)), nil
		}

		totals := map[int]float64{}
		counts := map[int]int{}
		names := map[int]string{}
		for _, inv := range invoices {
			dueDate, err := time.Parse(dateLayout, inv.DueDate)
			if err != nil {
				continue
			}
			if (dueDate.Equal(from) || dueDate.After(from)) && (dueDate.Equal(to) || dueDate.Before(to)) {
				totals[inv.SupplierNumber] += inv.Amount.Float()
				counts[inv.SupplierNumber]++
				names[inv.SupplierNumber] = inv.SupplierName
			}
		}

		ranks := make([]supplierRank, 0, len(totals))
		for num, total := range totals {
			ranks = append(ranks, supplierRank{
				SupplierNumber: num,
				SupplierName:   names[num],
				Count:          counts[num],
				Total:          total,
			})
		}
		sort.Slice(ranks, func(i, j int) bool {
			return ranks[i].Total > ranks[j].Total
		})

		if limit > 0 && limit < len(ranks) {
			ranks = ranks[:limit]
		}

		return jsonResult(ranks)
	}
}

// --- sales_vs_purchases ---

type salesVsPurchasesResult struct {
	Revenue  float64 `json:"revenue"`
	COGS     float64 `json:"cogs"`
	Net      float64 `json:"net"`
	From     string  `json:"from"`
	To       string  `json:"to"`
}

func salesVsPurchasesHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		fromStr, _ := args["from_date"].(string)
		toStr, _ := args["to_date"].(string)

		from, err := parseDate(fromStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("from_date: %v", err)), nil
		}
		to, err := parseDate(toStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("to_date: %v", err)), nil
		}

		years, err := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch financial years: %v", err)), nil
		}
		yearID := latestYearID(years)

		m, err := periodMetrics(ctx, deps, yearID, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period metrics: %v", err)), nil
		}

		return jsonResult(salesVsPurchasesResult{
			Revenue: m.Revenue,
			COGS:    m.COGS,
			Net:     m.Revenue - m.COGS,
			From:    fromStr,
			To:      toStr,
		})
	}
}

// --- company_info ---

func companyInfoHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		company, err := deps.CompanyInf.Info(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch company info: %v", err)), nil
		}
		return jsonResult(company)
	}
}
