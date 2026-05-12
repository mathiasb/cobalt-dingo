// Package ui provides HTTP handlers and templ templates for the web interface.
package ui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/payment"
)

// Server handles HTTP requests for the cobalt-dingo UI.
type Server struct {
	debtor   config.Debtor
	invoices domain.InvoiceSource
	enricher domain.SupplierEnricher
	batches  *domain.BatchService // nil when DB is not configured
	sessions *auth.SessionManager
	log      *slog.Logger
}

// NewServer constructs a Server wired to the given domain ports.
// batches may be nil; submission endpoints return 503 when it is absent.
func NewServer(
	debtor config.Debtor,
	invoices domain.InvoiceSource,
	enricher domain.SupplierEnricher,
	batches *domain.BatchService,
	sessions *auth.SessionManager,
	log *slog.Logger,
) *Server {
	return &Server{
		debtor:   debtor,
		invoices: invoices,
		enricher: enricher,
		batches:  batches,
		sessions: sessions,
		log:      log,
	}
}

// tenantID resolves the per-request tenant from the session.
// Falls back to "default" when auth is disabled (dev mode without OIDC).
func (s *Server) tenantID(r *http.Request) domain.TenantID {
	if sess := auth.FromContext(r); sess != nil {
		return domain.TenantID(sess.TenantID())
	}
	return domain.TenantID("default")
}

// RegisterRoutes wires the UI handlers onto mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /invoices", s.invoicesHandler)
	mux.HandleFunc("GET /", s.invoicesHandler)
	mux.HandleFunc("POST /invoices/batch", s.batchHandler)
	mux.HandleFunc("GET /invoices/batch/download", s.downloadHandler)
	mux.HandleFunc("POST /invoices/batch/submit", s.submitHandler)
	mux.HandleFunc("POST /settings/mode", s.modeHandler)
}

// modeHandler switches the active Fortnox mode in the session cookie.
func (s *Server) modeHandler(w http.ResponseWriter, r *http.Request) {
	sess := auth.FromContext(r)
	if sess == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	m := config.Mode(r.FormValue("mode"))
	if !m.IsValid() {
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}
	sess.Mode = m
	if err := s.sessions.Set(w, *sess); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, r.Referer(), http.StatusSeeOther)
}

// userNavFrom builds a UserNav from the current session, or nil in dev mode.
func userNavFrom(r *http.Request) *UserNav {
	s := auth.FromContext(r)
	if s == nil {
		return nil
	}
	return &UserNav{Name: s.Name, Email: s.Email, Mode: s.Mode}
}

func (s *Server) invoicesHandler(w http.ResponseWriter, r *http.Request) {
	invoices, err := s.loadPendingInvoices(r)
	if err != nil {
		s.log.Error("load invoices", "err", err)
		http.Error(w, "failed to load invoices from Fortnox", http.StatusBadGateway)
		return
	}
	render(w, r, InvoicesPage(invoices, userNavFrom(r)))
}

func (s *Server) batchHandler(w http.ResponseWriter, r *http.Request) {
	invoices, err := s.loadPendingInvoices(r)
	if err != nil {
		s.log.Error("load invoices for batch", "err", err)
		http.Error(w, "failed to load invoices from Fortnox", http.StatusBadGateway)
		return
	}
	debtor := payment.Debtor{Name: s.debtor.Name, IBAN: s.debtor.IBAN, BIC: s.debtor.BIC}
	summary, err := buildBatchSummary(r.Context(), invoices, debtor, s.tenantID(r), s.batches)
	if err != nil {
		s.log.Error("build batch", "err", err)
		http.Error(w, fmt.Sprintf("batch generation failed: %v", err), http.StatusInternalServerError)
		return
	}
	render(w, r, BatchPanel(summary))
}

func (s *Server) submitHandler(w http.ResponseWriter, r *http.Request) {
	if s.batches == nil {
		http.Error(w, "batch submission requires database — set DATABASE_URL", http.StatusServiceUnavailable)
		return
	}
	batchID := domain.BatchID(r.FormValue("batch_id"))
	if batchID == "" {
		http.Error(w, "batch_id required", http.StatusBadRequest)
		return
	}
	ref, err := s.batches.Submit(r.Context(), s.tenantID(r), batchID)
	if err != nil {
		s.log.Error("submit batch", "batch_id", batchID, "err", err)
		http.Error(w, fmt.Sprintf("submit failed: %v", err), http.StatusInternalServerError)
		return
	}
	render(w, r, SubmitConfirmation(string(batchID), string(ref)))
}

func (s *Server) downloadHandler(w http.ResponseWriter, r *http.Request) {
	invoices, err := s.loadPendingInvoices(r)
	if err != nil {
		s.log.Error("load invoices for download", "err", err)
		http.Error(w, "failed to load invoices from Fortnox", http.StatusBadGateway)
		return
	}
	debtor := payment.Debtor{Name: s.debtor.Name, IBAN: s.debtor.IBAN, BIC: s.debtor.BIC}
	summary, err := buildBatchSummary(r.Context(), invoices, debtor, s.tenantID(r), s.batches)
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
func (s *Server) loadPendingInvoices(r *http.Request) ([]PendingInvoice, error) {
	tid := s.tenantID(r)
	ctx := r.Context()
	all, err := s.invoices.UnpaidInvoices(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("fetch invoices: %w", err)
	}

	var queue domain.Queue
	domain.Sync(all, &queue)
	fcyInvoices := queue.All()

	if len(fcyInvoices) == 0 {
		return nil, nil
	}

	enriched, err := domain.Enrich(fcyInvoices, cachedLookup(ctx, tid, s.enricher))
	if err != nil {
		return nil, fmt.Errorf("enrich invoices: %w", err)
	}

	pending := make([]PendingInvoice, len(enriched))
	for i, inv := range enriched {
		pending[i] = toPendingInvoice(inv)
	}
	return pending, nil
}

// cachedLookup returns a SupplierLookup that deduplicates API calls per supplier
// within a single request.
func cachedLookup(ctx context.Context, tenantID domain.TenantID, e domain.SupplierEnricher) domain.SupplierLookup {
	cache := map[int][2]string{}
	return func(supplierNumber int) (string, string, error) {
		if hit, ok := cache[supplierNumber]; ok {
			return hit[0], hit[1], nil
		}
		iban, bic, err := e.SupplierPaymentDetails(ctx, tenantID, supplierNumber)
		if err != nil {
			return "", "", fmt.Errorf("supplier payment details %d: %w", supplierNumber, err)
		}
		cache[supplierNumber] = [2]string{iban, bic}
		return iban, bic, nil
	}
}

func toPendingInvoice(inv domain.EnrichedInvoice) PendingInvoice {
	return PendingInvoice{
		InvoiceNumber: inv.InvoiceNumber,
		Supplier:      inv.SupplierName,
		Currency:      inv.Amount.Currency,
		Amount:        inv.Amount.Float(),
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

// buildBatchSummary generates the PAIN.001 XML, groups invoices by currency,
// and — when batchSvc is non-nil — persists the batch as a draft in the DB,
// populating BatchSummary.BatchID to enable the submit button.
func buildBatchSummary(ctx context.Context, invoices []PendingInvoice, debtor payment.Debtor, tenantID domain.TenantID, batchSvc *domain.BatchService) (BatchSummary, error) {
	msgID := fmt.Sprintf("COBALT-%s", time.Now().UTC().Format("20060102-150405"))

	enriched := make([]domain.EnrichedInvoice, len(invoices))
	for i, inv := range invoices {
		enriched[i] = domain.EnrichedInvoice{
			SupplierInvoice: domain.SupplierInvoice{
				InvoiceNumber: inv.InvoiceNumber,
				SupplierName:  inv.Supplier,
				Amount:        domain.MoneyFromFloat(inv.Amount, inv.Currency),
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

	summary := BatchSummary{MsgID: msgID, Groups: groups, XML: xmlBytes}

	// Persist as draft when DB is configured; enables the submit button.
	if batchSvc != nil {
		items := make([]domain.BatchItem, len(enriched))
		for i, inv := range enriched {
			items[i] = domain.BatchItem{
				FortnoxInvoiceNumber: inv.InvoiceNumber,
				SupplierName:         inv.SupplierName,
				SupplierIBAN:         inv.IBAN,
				SupplierBIC:          inv.BIC,
				Amount:               inv.Amount,
				DueDate:              inv.DueDate,
			}
		}
		saved, saveErr := batchSvc.SaveDraft(ctx, tenantID, msgID, items, xmlBytes)
		if saveErr == nil {
			summary.BatchID = string(saved.ID)
		}
		// Non-fatal: batch still generated, submit button just won't appear.
	}

	return summary, nil
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	if err := c.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
