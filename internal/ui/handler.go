// Package ui provides HTTP handlers and templ templates for the web interface.
package ui

import (
	"net/http"

	"github.com/a-h/templ"
)

// fakeInvoices is placeholder data until the real Fortnox connector is wired to the UI.
var fakeInvoices = []PendingInvoice{
	{InvoiceNumber: 1042, Supplier: "Acme GmbH", Currency: "EUR", Amount: 2450.00, DueDate: "2026-05-03"},
	{InvoiceNumber: 1043, Supplier: "Nordic Supply AB", Currency: "USD", Amount: 1890.00, DueDate: "2026-05-10"},
	{InvoiceNumber: 1044, Supplier: "London Parts Ltd", Currency: "GBP", Amount: 3200.00, DueDate: "2026-04-14", Overdue: true},
	{InvoiceNumber: 1045, Supplier: "Swiss Precision SA", Currency: "CHF", Amount: 890.00, DueDate: "2026-05-20"},
}

// RegisterRoutes wires the UI handlers onto mux.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /invoices", invoicesHandler)
	mux.HandleFunc("GET /", invoicesHandler)
	mux.HandleFunc("POST /invoices/batch", batchHandler)
}

func invoicesHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, InvoicesPage(fakeInvoices))
}

func batchHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, BatchPanel(fakeInvoices))
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	if err := c.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
