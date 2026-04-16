// Package ui provides HTTP handlers and templ templates for the web interface.
package ui

import (
	"fmt"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/mathiasb/cobalt-dingo/internal/invoice"
	"github.com/mathiasb/cobalt-dingo/internal/payment"
)

// fakeInvoices is placeholder data until the real Fortnox connector is wired to the UI.
var fakeInvoices = []PendingInvoice{
	{InvoiceNumber: 1042, Supplier: "Acme GmbH", Currency: "EUR", Amount: 2450.00, DueDate: "2026-05-03", IBAN: "DE89370400440532013000", BIC: "COBADEFFXXX"},
	{InvoiceNumber: 1043, Supplier: "Nordic Supply AB", Currency: "USD", Amount: 1890.00, DueDate: "2026-05-10", IBAN: "GB29NWBK60161331926819", BIC: "NWBKGB2L"},
	{InvoiceNumber: 1044, Supplier: "London Parts Ltd", Currency: "GBP", Amount: 3200.00, DueDate: "2026-04-14", Overdue: true, IBAN: "GB29NWBK60161331926820", BIC: "NWBKGB2L"},
	{InvoiceNumber: 1045, Supplier: "Swiss Precision SA", Currency: "CHF", Amount: 890.00, DueDate: "2026-05-20", IBAN: "CH9300762011623852957", BIC: "UBSWCHZH"},
}

// fakeDebtor is the placeholder paying entity.
var fakeDebtor = payment.Debtor{
	Name: "Cobalt Dingo AB",
	IBAN: "SE4550000000058398257466",
	BIC:  "ESSESESS",
}

// RegisterRoutes wires the UI handlers onto mux.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /invoices", invoicesHandler)
	mux.HandleFunc("GET /", invoicesHandler)
	mux.HandleFunc("POST /invoices/batch", batchHandler)
	mux.HandleFunc("GET /invoices/batch/download", downloadHandler)
}

func invoicesHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, InvoicesPage(fakeInvoices))
}

func batchHandler(w http.ResponseWriter, r *http.Request) {
	summary, err := buildBatchSummary(fakeInvoices, fakeDebtor)
	if err != nil {
		http.Error(w, fmt.Sprintf("batch generation failed: %v", err), http.StatusInternalServerError)
		return
	}
	render(w, r, BatchPanel(summary))
}

func downloadHandler(w http.ResponseWriter, _ *http.Request) {
	summary, err := buildBatchSummary(fakeInvoices, fakeDebtor)
	if err != nil {
		http.Error(w, fmt.Sprintf("batch generation failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="payment-batch.xml"`)
	_, _ = w.Write(summary.XML)
}

func buildBatchSummary(invoices []PendingInvoice, debtor payment.Debtor) (BatchSummary, error) {
	msgID := fmt.Sprintf("COBALT-%s", time.Now().UTC().Format("20060102-150405"))

	enriched := make([]invoice.EnrichedInvoice, len(invoices))
	for i, inv := range invoices {
		enriched[i] = invoice.EnrichedInvoice{
			SupplierInvoice: invoice.SupplierInvoice{
				InvoiceNumber: inv.InvoiceNumber,
				SupplierName:  inv.Supplier,
				Currency:      inv.Currency,
				Total:         inv.Amount,
				DueDate:       inv.DueDate,
			},
			IBAN: inv.IBAN,
			BIC:  inv.BIC,
		}
	}

	xmlBytes, err := payment.GeneratePAIN001(enriched, debtor, msgID, time.Now().UTC())
	if err != nil {
		return BatchSummary{}, fmt.Errorf("generate pain.001: %w", err)
	}

	// Group invoices by currency for display.
	order := []string{}
	grouped := map[string][]PendingInvoice{}
	for _, inv := range invoices {
		if _, seen := grouped[inv.Currency]; !seen {
			order = append(order, inv.Currency)
		}
		grouped[inv.Currency] = append(grouped[inv.Currency], inv)
	}

	groups := make([]BatchGroup, 0, len(order))
	for _, ccy := range order {
		ccyInvs := grouped[ccy]
		var total float64
		for _, inv := range ccyInvs {
			total += inv.Amount
		}
		groups = append(groups, BatchGroup{
			Currency: ccy,
			Invoices: ccyInvs,
			Total:    total,
		})
	}

	return BatchSummary{
		MsgID:  msgID,
		Groups: groups,
		XML:    xmlBytes,
	}, nil
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	if err := c.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
