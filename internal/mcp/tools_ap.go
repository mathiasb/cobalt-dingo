package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/analyst"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

func registerAPTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("ap_summary",
		mcp.WithDescription("Summarise unpaid supplier invoices: count and totals grouped by currency, plus the oldest due date."),
	), apSummaryHandler(deps))

	s.AddTool(mcp.NewTool("ap_overdue",
		mcp.WithDescription("List overdue supplier invoices, sorted by days overdue descending."),
		mcp.WithNumber("days_threshold",
			mcp.Description("Only include invoices overdue by at least this many days (default 0)."),
		),
	), apOverdueHandler(deps))

	s.AddTool(mcp.NewTool("ap_by_supplier",
		mcp.WithDescription("Group unpaid invoices by supplier, sorted by total amount descending."),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of suppliers to return (default: all)."),
		),
	), apBySupplierHandler(deps))

	s.AddTool(mcp.NewTool("ap_by_currency",
		mcp.WithDescription("Group unpaid invoices by currency with counts and totals."),
	), apByCurrencyHandler(deps))

	s.AddTool(mcp.NewTool("ap_aging",
		mcp.WithDescription("Aging report for unpaid supplier invoices bucketed into Current, 1-30, 31-60, 61-90 and 90+ days."),
	), apAgingHandler(deps))

	s.AddTool(mcp.NewTool("ap_supplier_history",
		mcp.WithDescription("Supplier master data plus all outstanding invoices for a given supplier."),
		mcp.WithNumber("supplier_number",
			mcp.Description("Fortnox supplier number."),
			mcp.Required(),
		),
	), apSupplierHistoryHandler(deps))

	s.AddTool(mcp.NewTool("ap_invoice_detail",
		mcp.WithDescription("Detail and payment history for a single supplier invoice."),
		mcp.WithNumber("invoice_number",
			mcp.Description("Fortnox invoice number."),
			mcp.Required(),
		),
	), apInvoiceDetailHandler(deps))
}

// jsonResult marshals v to JSON and wraps it in a text tool result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// --- ap_summary ---

type currencySummary struct {
	Currency   string  `json:"currency"`
	Count      int     `json:"count"`
	TotalFloat float64 `json:"total"`
	OldestDue  string  `json:"oldest_due,omitempty"`
}

type apSummaryResult struct {
	TotalCount int               `json:"total_count"`
	Currencies []currencySummary `json:"currencies"`
}

func apSummaryHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		grouped := analyst.GroupBy(invoices, func(inv domain.SupplierInvoice) string {
			return inv.Amount.Currency
		})

		var summaries []currencySummary
		for _, currency := range analyst.OrderedKeys(invoices, func(inv domain.SupplierInvoice) string {
			return inv.Amount.Currency
		}) {
			grp := grouped[currency]
			var total int64
			oldest := ""
			for _, inv := range grp {
				total += inv.Amount.MinorUnits
				if oldest == "" || inv.DueDate < oldest {
					oldest = inv.DueDate
				}
			}
			summaries = append(summaries, currencySummary{
				Currency:   currency,
				Count:      len(grp),
				TotalFloat: float64(total) / 100,
				OldestDue:  oldest,
			})
		}

		return jsonResult(apSummaryResult{
			TotalCount: len(invoices),
			Currencies: summaries,
		})
	}
}

// --- ap_overdue ---

type overdueInvoice struct {
	InvoiceNumber  int     `json:"invoice_number"`
	SupplierNumber int     `json:"supplier_number"`
	SupplierName   string  `json:"supplier_name"`
	Currency       string  `json:"currency"`
	Amount         float64 `json:"amount"`
	DueDate        string  `json:"due_date"`
	DaysOverdue    int     `json:"days_overdue"`
}

func apOverdueHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		threshold := req.GetInt("days_threshold", 0)

		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		today := time.Now()
		var overdue []overdueInvoice
		for _, inv := range invoices {
			days := analyst.DaysOverdue(inv.DueDate, today)
			if days >= threshold && days > 0 {
				overdue = append(overdue, overdueInvoice{
					InvoiceNumber:  inv.InvoiceNumber,
					SupplierNumber: inv.SupplierNumber,
					SupplierName:   inv.SupplierName,
					Currency:       inv.Amount.Currency,
					Amount:         inv.Amount.Float(),
					DueDate:        inv.DueDate,
					DaysOverdue:    days,
				})
			}
		}

		sort.Slice(overdue, func(i, j int) bool {
			return overdue[i].DaysOverdue > overdue[j].DaysOverdue
		})

		return jsonResult(overdue)
	}
}

// --- ap_by_supplier ---

type supplierGroup struct {
	SupplierNumber int     `json:"supplier_number"`
	SupplierName   string  `json:"supplier_name"`
	Count          int     `json:"count"`
	TotalSEK       float64 `json:"total_sek,omitempty"`
	Currencies     []string `json:"currencies"`
}

func apBySupplierHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := req.GetInt("limit", 0)

		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		grouped := analyst.GroupBy(invoices, func(inv domain.SupplierInvoice) int {
			return inv.SupplierNumber
		})

		var groups []supplierGroup
		for _, num := range analyst.OrderedKeys(invoices, func(inv domain.SupplierInvoice) int {
			return inv.SupplierNumber
		}) {
			grp := grouped[num]
			var total int64
			currencySet := map[string]struct{}{}
			name := ""
			for _, inv := range grp {
				total += inv.Amount.MinorUnits
				currencySet[inv.Amount.Currency] = struct{}{}
				name = inv.SupplierName
			}
			var currencies []string
			for c := range currencySet {
				currencies = append(currencies, c)
			}
			sort.Strings(currencies)
			groups = append(groups, supplierGroup{
				SupplierNumber: num,
				SupplierName:   name,
				Count:          len(grp),
				TotalSEK:       float64(total) / 100,
				Currencies:     currencies,
			})
		}

		sort.Slice(groups, func(i, j int) bool {
			return groups[i].TotalSEK > groups[j].TotalSEK
		})

		if limit > 0 && limit < len(groups) {
			groups = groups[:limit]
		}

		return jsonResult(groups)
	}
}

// --- ap_by_currency ---

type currencyGroup struct {
	Currency string  `json:"currency"`
	Count    int     `json:"count"`
	Total    float64 `json:"total"`
}

func apByCurrencyHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		grouped := analyst.GroupBy(invoices, func(inv domain.SupplierInvoice) string {
			return inv.Amount.Currency
		})

		var groups []currencyGroup
		for _, currency := range analyst.OrderedKeys(invoices, func(inv domain.SupplierInvoice) string {
			return inv.Amount.Currency
		}) {
			grp := grouped[currency]
			var total int64
			for _, inv := range grp {
				total += inv.Amount.MinorUnits
			}
			groups = append(groups, currencyGroup{
				Currency: currency,
				Count:    len(grp),
				Total:    float64(total) / 100,
			})
		}

		return jsonResult(groups)
	}
}

// --- ap_aging ---

func apAgingHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		dueDates := make([]string, len(invoices))
		for i, inv := range invoices {
			dueDates[i] = inv.DueDate
		}

		report := analyst.AgingBuckets(dueDates, time.Now())
		return jsonResult(report)
	}
}

// --- ap_supplier_history ---

type supplierHistoryResult struct {
	Supplier domain.Supplier          `json:"supplier"`
	Invoices []domain.SupplierInvoice `json:"invoices"`
}

func apSupplierHistoryHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		supplierNumber, err := req.RequireInt("supplier_number")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("supplier_number required: %v", err)), nil
		}

		supplier, err := deps.SupplierLdg.SupplierDetail(ctx, deps.TenantID, supplierNumber)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch supplier: %v", err)), nil
		}

		all, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		var invoices []domain.SupplierInvoice
		for _, inv := range all {
			if inv.SupplierNumber == supplierNumber {
				invoices = append(invoices, inv)
			}
		}

		return jsonResult(supplierHistoryResult{
			Supplier: supplier,
			Invoices: invoices,
		})
	}
}

// --- ap_invoice_detail ---

type invoiceDetailResult struct {
	Invoice  *domain.SupplierInvoice  `json:"invoice,omitempty"`
	Payments []domain.SupplierPayment `json:"payments"`
}

func apInvoiceDetailHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoiceNumber, err := req.RequireInt("invoice_number")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invoice_number required: %v", err)), nil
		}

		all, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		var found *domain.SupplierInvoice
		for i, inv := range all {
			if inv.InvoiceNumber == invoiceNumber {
				found = &all[i]
				break
			}
		}

		payments, err := deps.SupplierLdg.InvoicePayments(ctx, deps.TenantID, invoiceNumber)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch payments: %v", err)), nil
		}

		return jsonResult(invoiceDetailResult{
			Invoice:  found,
			Payments: payments,
		})
	}
}
