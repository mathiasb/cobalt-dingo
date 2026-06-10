package ui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fortnoxClientAdapter wraps a real *fortnox.Client (pointed at an httptest
// server) so it satisfies the domain ports the handler depends on. It exists
// only in tests: it lets the handler drive the genuine Fortnox JSON decode
// path — including FlexInt — without the production config.Fortnox.BaseURL()
// hardcode that the real adapter.Connector is bound to.
type fortnoxClientAdapter struct{ c *fortnox.Client }

func (a fortnoxClientAdapter) UnpaidInvoices(_ context.Context, _ domain.TenantID) ([]domain.SupplierInvoice, error) {
	return a.c.UnpaidSupplierInvoices()
}

func (a fortnoxClientAdapter) SupplierPaymentDetails(_ context.Context, _ domain.TenantID, supplierNumber int) (string, string, error) {
	return a.c.SupplierPaymentDetails(supplierNumber)
}

// newFortnoxBackedServer builds a Server whose InvoiceSource and
// SupplierEnricher both delegate to a real fortnox.Client talking to the given
// fake-Fortnox httptest server. batches and sessions are nil: the read path
// (/invoices) touches neither.
func newFortnoxBackedServer(t *testing.T, fortnoxURL string) *Server {
	t.Helper()
	client := fortnox.NewClient(fortnoxURL, "test-token", true)
	adapter := fortnoxClientAdapter{c: client}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(config.Debtor{}, adapter, adapter, nil, nil, log)
}

// getInvoices drives a GET /invoices request through the full registered route
// table and returns the recorder.
func getInvoices(s *Server) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	r := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// Realistic Fortnox shapes using the QUOTED-STRING numeric forms that blocked
// #26 — InvoiceNumber/SupplierNumber arrive as quoted strings and must decode
// through FlexInt. A SEK invoice is included to confirm the FCY filter drops it.
const fortnoxInvoicesQuoted = `{"SupplierInvoices":[
	{"InvoiceNumber":"1042","SupplierNumber":"1","SupplierName":"Acme GmbH","Currency":"EUR","TotalInvoiceCurrency":2450.00,"DueDate":"2026-05-03"},
	{"InvoiceNumber":"1043","SupplierNumber":"2","SupplierName":"Nordic Supply AB","Currency":"USD","TotalInvoiceCurrency":1890.00,"DueDate":"2026-05-10"},
	{"InvoiceNumber":"9001","SupplierNumber":"3","SupplierName":"Svensk Leverantor AB","Currency":"SEK","TotalInvoiceCurrency":5000.00,"DueDate":"2026-05-15"}
]}`

func supplierResponseQuoted(supplierNumber, iban, bic string) string {
	return `{"Supplier":{"SupplierNumber":"` + supplierNumber + `","IBAN":"` + iban + `","BIC":"` + bic + `"}}`
}

// TestInvoicesHandler_Success_QuotedShapes is the case that would have caught
// #26: the handler runs fetch → FCY filter → enrich → render against a fake
// Fortnox returning quoted-string numerics, decoded through FlexInt, and the
// page renders 200 with the FCY invoice rows.
func TestInvoicesHandler_Success_QuotedShapes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/3/supplierinvoices"):
			_, _ = io.WriteString(w, fortnoxInvoicesQuoted)
		case r.URL.Path == "/3/suppliers/1":
			_, _ = io.WriteString(w, supplierResponseQuoted("1", "DE89370400440532013000", "COBADEFFXXX"))
		case r.URL.Path == "/3/suppliers/2":
			_, _ = io.WriteString(w, supplierResponseQuoted("2", "GB29NWBK60161331926819", "NWBKGB2L"))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	w := getInvoices(newFortnoxBackedServer(t, srv.URL))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	// FCY rows present, decoded through the FlexInt (quoted-number) path.
	assert.Contains(t, body, "Acme GmbH")
	assert.Contains(t, body, "#1042")
	assert.Contains(t, body, "EUR 2,450.00")
	assert.Contains(t, body, "Nordic Supply AB")
	assert.Contains(t, body, "#1043")
	// SEK invoice filtered out before enrichment.
	assert.NotContains(t, body, "Svensk Leverantor AB")
}

// TestInvoicesHandler_GracefulError verifies that an upstream Fortnox failure
// during enrichment surfaces as a clean 502 — exactly how #26 manifested in
// production — rather than a panic or a 500 render error.
func TestInvoicesHandler_GracefulError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/3/supplierinvoices"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, fortnoxInvoicesQuoted)
		case strings.HasPrefix(r.URL.Path, "/3/suppliers/"):
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	w := getInvoices(newFortnoxBackedServer(t, srv.URL))

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "failed to load invoices from Fortnox")
}

// TestInvoicesHandler_Empty verifies the zero-FCY-invoice path
// (loadPendingInvoices returns nil, nil): the page renders 200 with no rows
// and no nil-slice panic. The fake returns only a SEK invoice, which the FCY
// filter drops, so enrichment is never reached.
func TestInvoicesHandler_Empty(t *testing.T) {
	const sekOnly = `{"SupplierInvoices":[
		{"InvoiceNumber":"9001","SupplierNumber":"3","SupplierName":"Svensk Leverantor AB","Currency":"SEK","TotalInvoiceCurrency":5000.00,"DueDate":"2026-05-15"}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/3/supplierinvoices") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, sekOnly)
			return
		}
		http.Error(w, "supplier endpoint must not be called when no FCY invoices", http.StatusNotFound)
	}))
	defer srv.Close()

	w := getInvoices(newFortnoxBackedServer(t, srv.URL))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Pending payments") // page chrome renders
	assert.NotContains(t, body, "Svensk Leverantor AB")
}
