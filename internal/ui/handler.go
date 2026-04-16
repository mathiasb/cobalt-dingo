// Package ui provides HTTP handlers and templ templates for the web interface.
package ui

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/invoice"
	"github.com/mathiasb/cobalt-dingo/internal/payment"
)

// fakeInvoices is used as fallback when Fortnox is not configured (dev without .env).
var fakeInvoices = []PendingInvoice{
	{InvoiceNumber: 1042, Supplier: "Acme GmbH", Currency: "EUR", Amount: 2450.00, DueDate: "2026-05-03", IBAN: "DE89370400440532013000", BIC: "COBADEFFXXX"},
	{InvoiceNumber: 1043, Supplier: "Nordic Supply AB", Currency: "USD", Amount: 1890.00, DueDate: "2026-05-10", IBAN: "GB29NWBK60161331926819", BIC: "NWBKGB2L"},
	{InvoiceNumber: 1044, Supplier: "London Parts Ltd", Currency: "GBP", Amount: 3200.00, DueDate: "2026-04-14", Overdue: true, IBAN: "GB29NWBK60161331926820", BIC: "NWBKGB2L"},
	{InvoiceNumber: 1045, Supplier: "Swiss Precision SA", Currency: "CHF", Amount: 890.00, DueDate: "2026-05-20", IBAN: "CH9300762011623852957", BIC: "UBSWCHZH"},
}

// Server handles HTTP requests for the cobalt-dingo UI.
type Server struct {
	cfg    config.Fortnox
	debtor config.Debtor
	log    *slog.Logger
}

// NewServer constructs a Server with the given configuration.
func NewServer(cfg config.Fortnox, debtor config.Debtor, log *slog.Logger) *Server {
	return &Server{cfg: cfg, debtor: debtor, log: log}
}

// RegisterRoutes wires the UI handlers onto mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /invoices", s.invoicesHandler)
	mux.HandleFunc("GET /", s.invoicesHandler)
	mux.HandleFunc("POST /invoices/batch", s.batchHandler)
	mux.HandleFunc("GET /invoices/batch/download", s.downloadHandler)
}

func (s *Server) invoicesHandler(w http.ResponseWriter, r *http.Request) {
	invoices, err := s.loadPendingInvoices()
	if err != nil {
		s.log.Error("load invoices", "err", err)
		http.Error(w, "failed to load invoices from Fortnox", http.StatusBadGateway)
		return
	}
	render(w, r, InvoicesPage(invoices))
}

func (s *Server) batchHandler(w http.ResponseWriter, r *http.Request) {
	invoices, err := s.loadPendingInvoices()
	if err != nil {
		s.log.Error("load invoices for batch", "err", err)
		http.Error(w, "failed to load invoices from Fortnox", http.StatusBadGateway)
		return
	}
	debtor := payment.Debtor{Name: s.debtor.Name, IBAN: s.debtor.IBAN, BIC: s.debtor.BIC}
	summary, err := buildBatchSummary(invoices, debtor)
	if err != nil {
		s.log.Error("build batch", "err", err)
		http.Error(w, fmt.Sprintf("batch generation failed: %v", err), http.StatusInternalServerError)
		return
	}
	render(w, r, BatchPanel(summary))
}

func (s *Server) downloadHandler(w http.ResponseWriter, _ *http.Request) {
	invoices, err := s.loadPendingInvoices()
	if err != nil {
		s.log.Error("load invoices for download", "err", err)
		http.Error(w, "failed to load invoices from Fortnox", http.StatusBadGateway)
		return
	}
	debtor := payment.Debtor{Name: s.debtor.Name, IBAN: s.debtor.IBAN, BIC: s.debtor.BIC}
	summary, err := buildBatchSummary(invoices, debtor)
	if err != nil {
		s.log.Error("build batch for download", "err", err)
		http.Error(w, fmt.Sprintf("batch generation failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="payment-batch.xml"`)
	_, _ = w.Write(summary.XML)
}

// loadPendingInvoices runs the full pipeline: fetch → filter FCY → enrich with IBAN/BIC.
// Falls back to fakeInvoices when Fortnox is not configured (ClientID absent).
func (s *Server) loadPendingInvoices() ([]PendingInvoice, error) {
	if s.cfg.ClientID == "" {
		return fakeInvoices, nil
	}

	client, err := s.fortnoxClient()
	if err != nil {
		return nil, fmt.Errorf("fortnox client: %w", err)
	}

	all, err := client.UnpaidSupplierInvoices()
	if err != nil {
		return nil, fmt.Errorf("fetch invoices: %w", err)
	}

	var queue invoice.Queue
	invoice.Sync(all, &queue)
	fcyInvoices := queue.All()

	if len(fcyInvoices) == 0 {
		return nil, nil
	}

	enriched, err := invoice.Enrich(fcyInvoices, cachedLookup(client))
	if err != nil {
		return nil, fmt.Errorf("enrich invoices: %w", err)
	}

	pending := make([]PendingInvoice, len(enriched))
	for i, inv := range enriched {
		pending[i] = toPendingInvoice(inv)
	}
	return pending, nil
}

// fortnoxClient loads the stored token, refreshing it if expired, and returns a client.
func (s *Server) fortnoxClient() (*fortnox.Client, error) {
	tok, err := fortnox.LoadToken()
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	if !tok.Valid() {
		tok, err = fortnox.RefreshAccessToken(s.cfg.ClientID, s.cfg.ClientSecret, tok.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("refresh token: %w", err)
		}
		if saveErr := fortnox.SaveToken(tok); saveErr != nil {
			s.log.Warn("failed to persist refreshed token", "err", saveErr)
		}
	}
	return fortnox.NewClient(s.cfg.BaseURL(), tok.AccessToken), nil
}

// cachedLookup returns a SupplierLookup that deduplicates API calls per supplier
// within a single request.
func cachedLookup(client *fortnox.Client) invoice.SupplierLookup {
	cache := map[int][2]string{}
	return func(supplierNumber int) (string, string, error) {
		if hit, ok := cache[supplierNumber]; ok {
			return hit[0], hit[1], nil
		}
		iban, bic, err := client.SupplierPaymentDetails(supplierNumber)
		if err != nil {
			return "", "", fmt.Errorf("supplier payment details %d: %w", supplierNumber, err)
		}
		cache[supplierNumber] = [2]string{iban, bic}
		return iban, bic, nil
	}
}

func toPendingInvoice(inv invoice.EnrichedInvoice) PendingInvoice {
	return PendingInvoice{
		InvoiceNumber: inv.InvoiceNumber,
		Supplier:      inv.SupplierName,
		Currency:      inv.Currency,
		Amount:        inv.Total,
		DueDate:       inv.DueDate,
		Overdue:       isOverdue(inv.DueDate),
		IBAN:          inv.IBAN,
		BIC:           inv.BIC,
	}
}

func isOverdue(dueDate string) bool {
	d, err := time.Parse("2006-01-02", dueDate)
	if err != nil {
		return false
	}
	return time.Now().After(d)
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

	return BatchSummary{MsgID: msgID, Groups: groups, XML: xmlBytes}, nil
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	if err := c.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
