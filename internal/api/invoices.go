package api

import (
	"context"
	"fmt"
	"time"
)

// Invoice represents a Fortnox customer invoice.
type Invoice struct {
	DocumentNumber string    `json:"DocumentNumber"`
	CustomerName   string    `json:"CustomerName"`
	CustomerNumber string    `json:"CustomerNumber"`
	InvoiceDate    string    `json:"InvoiceDate"`
	DueDate        string    `json:"DueDate"`
	Total          float64   `json:"Total"`
	Balance        float64   `json:"Balance"`
	Currency       string    `json:"Currency"`
	Sent           bool      `json:"Sent"`
	Booked         bool      `json:"Booked"`
	Cancelled      bool      `json:"Cancelled"`
	FinalPayDate   time.Time `json:"-"`
}

// IsPastDue reports whether the invoice is unpaid and past its due date.
func (inv *Invoice) IsPastDue() bool {
	return inv.Balance > 0 && !inv.Cancelled
}

type invoicesResponse struct {
	Invoices []Invoice `json:"Invoices"`
}

// ListInvoices returns invoices, optionally filtered.
// filter can be "unpaid", "unbooked", "cancelled", or "" for all.
func (c *Client) ListInvoices(ctx context.Context, filter string) ([]Invoice, error) {
	path := "invoices"
	if filter != "" {
		path += "?filter=" + filter
	}
	var resp invoicesResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("api: list invoices: %w", err)
	}
	return resp.Invoices, nil
}
