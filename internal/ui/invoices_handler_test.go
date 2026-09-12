package ui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTokenStore is a domain.TokenStore returning a fixed, valid token so the
// real Connector's token-load path runs without touching disk or the network.
type stubTokenStore struct{}

func (stubTokenStore) Load(_ context.Context, _ domain.TenantID) (domain.OAuthToken, error) {
	return domain.OAuthToken{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}, nil
}

func (stubTokenStore) Save(_ context.Context, _ domain.TenantID, _ domain.OAuthToken) error {
	return nil
}

func (stubTokenStore) AtomicRefresh(_ context.Context, _ domain.TenantID, _, _ domain.OAuthToken) error {
	return nil
}

func (stubTokenStore) Delete(_ context.Context, _ domain.TenantID) error { return nil }

// newFortnoxBackedServer builds a Server whose InvoiceSource and
// SupplierEnricher are both the real adapter.Connector, pointed at the given
// fake-Fortnox httptest server through the config base-URL override seam
// (cobalt-dingo#30). batches and sessions are nil: the read path (/invoices)
// touches neither.
func newFortnoxBackedServer(t *testing.T, fortnoxURL string) *Server {
	t.Helper()
	cfg := config.Fortnox{Mode: config.ModeProduction, BaseURLOverride: fortnoxURL}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	conn := adapterfortnox.NewConnector(cfg, stubTokenStore{}, log)
	return NewServer(config.Debtor{}, conn, conn, nil, nil, log)
}

// getInvoices drives a GET /invoices request through the full registered route
// table and returns the recorder.
//
// The request carries a session with a selected company. It used to carry none
// and rely on the tenant resolver defaulting to "default"; that fallback is
// gone, because with a company in the tenant key a default names a real
// company's books.
func getInvoices(s *Server) *httptest.ResponseRecorder {
	return getInvoicesAs(s, &auth.Session{
		Sub: "test-user", Mode: config.ModeSandbox, Company: "556677-8899",
	})
}

// getInvoicesAs drives the same request with an explicit session, so a test can
// exercise the no-company and no-session paths.
func getInvoicesAs(s *Server, sess *auth.Session) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	r := httptest.NewRequest(http.MethodGet, "/invoices", nil)
	if sess != nil {
		r = r.WithContext(auth.WithSession(r.Context(), sess))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// A logged-in user who has not picked a company yet is a normal state, not an
// error: send them to the chooser. Serving invoices would mean picking a
// company on their behalf.
func TestInvoicesHandler_redirectsWhenNoCompanySelected(t *testing.T) {
	fortnox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"SupplierInvoices":[]}`))
	}))
	defer fortnox.Close()

	w := getInvoicesAs(newFortnoxBackedServer(t, fortnox.URL),
		&auth.Session{Sub: "test-user", Mode: config.ModeSandbox})

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/fortnox/", w.Header().Get("Location"))
}

func TestInvoicesHandler_refusesWithNoSession(t *testing.T) {
	fortnox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"SupplierInvoices":[]}`))
	}))
	defer fortnox.Close()

	w := getInvoicesAs(newFortnoxBackedServer(t, fortnox.URL), nil)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
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
