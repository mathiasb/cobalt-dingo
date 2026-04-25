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

func registerARTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("ar_summary",
		mcp.WithDescription("Summarise unpaid customer invoices: count and totals grouped by currency, plus the oldest due date."),
	), arSummaryHandler(deps))

	s.AddTool(mcp.NewTool("ar_overdue",
		mcp.WithDescription("List overdue customer invoices, sorted by days overdue descending."),
		mcp.WithNumber("days_threshold",
			mcp.Description("Only include invoices overdue by at least this many days (default 0)."),
		),
	), arOverdueHandler(deps))

	s.AddTool(mcp.NewTool("ar_by_customer",
		mcp.WithDescription("Group unpaid customer invoices by customer, sorted by total amount descending."),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of customers to return (default: all)."),
		),
	), arByCustomerHandler(deps))

	s.AddTool(mcp.NewTool("ar_aging",
		mcp.WithDescription("Aging report for unpaid customer invoices bucketed into Current, 1-30, 31-60, 61-90 and 90+ days."),
	), arAgingHandler(deps))

	s.AddTool(mcp.NewTool("ar_customer_history",
		mcp.WithDescription("Customer master data plus all outstanding invoices for a given customer."),
		mcp.WithNumber("customer_number",
			mcp.Description("Fortnox customer number."),
			mcp.Required(),
		),
	), arCustomerHistoryHandler(deps))

	s.AddTool(mcp.NewTool("ar_invoice_detail",
		mcp.WithDescription("Detail and payment history for a single customer invoice."),
		mcp.WithNumber("invoice_number",
			mcp.Description("Fortnox invoice number."),
			mcp.Required(),
		),
	), arInvoiceDetailHandler(deps))

	s.AddTool(mcp.NewTool("ar_unpaid_report",
		mcp.WithDescription("Detailed overdue analysis with customer contact info (email, phone) for each overdue invoice."),
	), arUnpaidReportHandler(deps))
}

// --- ar_summary ---

type arSummaryResult struct {
	TotalCount int               `json:"total_count"`
	Currencies []currencySummary `json:"currencies"`
}

func arSummaryHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		grouped := analyst.GroupBy(invoices, func(inv domain.CustomerInvoice) string {
			return inv.Amount.Currency
		})

		var summaries []currencySummary
		for _, currency := range analyst.OrderedKeys(invoices, func(inv domain.CustomerInvoice) string {
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

		return jsonResult(arSummaryResult{
			TotalCount: len(invoices),
			Currencies: summaries,
		})
	}
}

// --- ar_overdue ---

type arOverdueInvoice struct {
	InvoiceNumber  int     `json:"invoice_number"`
	CustomerNumber int     `json:"customer_number"`
	CustomerName   string  `json:"customer_name"`
	Currency       string  `json:"currency"`
	Amount         float64 `json:"amount"`
	DueDate        string  `json:"due_date"`
	DaysOverdue    int     `json:"days_overdue"`
}

func arOverdueHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		threshold := req.GetInt("days_threshold", 0)

		invoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		today := time.Now()
		var overdue []arOverdueInvoice
		for _, inv := range invoices {
			days := analyst.DaysOverdue(inv.DueDate, today)
			if days >= threshold && days > 0 {
				overdue = append(overdue, arOverdueInvoice{
					InvoiceNumber:  inv.InvoiceNumber,
					CustomerNumber: inv.CustomerNumber,
					CustomerName:   inv.CustomerName,
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

// --- ar_by_customer ---

type customerGroup struct {
	CustomerNumber int      `json:"customer_number"`
	CustomerName   string   `json:"customer_name"`
	Count          int      `json:"count"`
	TotalSEK       float64  `json:"total_sek,omitempty"`
	Currencies     []string `json:"currencies"`
}

func arByCustomerHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := req.GetInt("limit", 0)

		invoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		grouped := analyst.GroupBy(invoices, func(inv domain.CustomerInvoice) int {
			return inv.CustomerNumber
		})

		var groups []customerGroup
		for _, num := range analyst.OrderedKeys(invoices, func(inv domain.CustomerInvoice) int {
			return inv.CustomerNumber
		}) {
			grp := grouped[num]
			var total int64
			currencySet := map[string]struct{}{}
			name := ""
			for _, inv := range grp {
				total += inv.Amount.MinorUnits
				currencySet[inv.Amount.Currency] = struct{}{}
				name = inv.CustomerName
			}
			var currencies []string
			for c := range currencySet {
				currencies = append(currencies, c)
			}
			sort.Strings(currencies)
			groups = append(groups, customerGroup{
				CustomerNumber: num,
				CustomerName:   name,
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

// --- ar_aging ---

func arAgingHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
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

// --- ar_customer_history ---

type customerHistoryResult struct {
	Customer domain.Customer         `json:"customer"`
	Invoices []domain.CustomerInvoice `json:"invoices"`
}

func arCustomerHistoryHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		customerNumber, err := req.RequireInt("customer_number")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("customer_number required: %v", err)), nil
		}

		customer, err := deps.CustomerLdg.CustomerDetail(ctx, deps.TenantID, customerNumber)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch customer: %v", err)), nil
		}

		all, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		var invoices []domain.CustomerInvoice
		for _, inv := range all {
			if inv.CustomerNumber == customerNumber {
				invoices = append(invoices, inv)
			}
		}

		return jsonResult(customerHistoryResult{
			Customer: customer,
			Invoices: invoices,
		})
	}
}

// --- ar_invoice_detail ---

type arInvoiceDetailResult struct {
	Invoice  *domain.CustomerInvoice  `json:"invoice,omitempty"`
	Payments []domain.CustomerPayment `json:"payments"`
}

func arInvoiceDetailHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoiceNumber, err := req.RequireInt("invoice_number")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invoice_number required: %v", err)), nil
		}

		all, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		var found *domain.CustomerInvoice
		for i, inv := range all {
			if inv.InvoiceNumber == invoiceNumber {
				found = &all[i]
				break
			}
		}

		payments, err := deps.CustomerLdg.InvoicePayments(ctx, deps.TenantID, invoiceNumber)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch payments: %v", err)), nil
		}

		return jsonResult(arInvoiceDetailResult{
			Invoice:  found,
			Payments: payments,
		})
	}
}

// --- ar_unpaid_report ---

type arUnpaidEntry struct {
	InvoiceNumber  int     `json:"invoice_number"`
	CustomerNumber int     `json:"customer_number"`
	CustomerName   string  `json:"customer_name"`
	Email          string  `json:"email"`
	Phone          string  `json:"phone"`
	Currency       string  `json:"currency"`
	Amount         float64 `json:"amount"`
	DueDate        string  `json:"due_date"`
	DaysOverdue    int     `json:"days_overdue"`
}

func arUnpaidReportHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		today := time.Now()

		// Cache customer details to avoid repeated lookups.
		customerCache := map[int]domain.Customer{}

		var entries []arUnpaidEntry
		for _, inv := range invoices {
			days := analyst.DaysOverdue(inv.DueDate, today)
			if days <= 0 {
				continue
			}

			cust, ok := customerCache[inv.CustomerNumber]
			if !ok {
				cust, err = deps.CustomerLdg.CustomerDetail(ctx, deps.TenantID, inv.CustomerNumber)
				if err != nil {
					// Tolerate missing detail — still report the invoice.
					cust = domain.Customer{CustomerNumber: inv.CustomerNumber, Name: inv.CustomerName}
				}
				customerCache[inv.CustomerNumber] = cust
			}

			entries = append(entries, arUnpaidEntry{
				InvoiceNumber:  inv.InvoiceNumber,
				CustomerNumber: inv.CustomerNumber,
				CustomerName:   inv.CustomerName,
				Email:          cust.Email,
				Phone:          cust.Phone,
				Currency:       inv.Amount.Currency,
				Amount:         inv.Amount.Float(),
				DueDate:        inv.DueDate,
				DaysOverdue:    days,
			})
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].DaysOverdue > entries[j].DaysOverdue
		})

		return jsonResult(entries)
	}
}
