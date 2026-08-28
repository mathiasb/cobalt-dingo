package fortnox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Realistic Fortnox shapes using the QUOTED-STRING numeric forms that took
// /invoices down in #26: InvoiceNumber and SupplierNumber arrive quoted and
// must survive FlexInt decoding. A SEK row is included so the caller-side FCY
// filter has something to drop — the Connector itself returns every row.
const connectorInvoicesQuoted = `{"SupplierInvoices":[
	{"InvoiceNumber":"1042","SupplierNumber":"1","SupplierName":"Acme GmbH","Currency":"EUR","TotalInvoiceCurrency":2450.00,"DueDate":"2026-05-03"},
	{"InvoiceNumber":1043,"SupplierNumber":2,"SupplierName":"Nordic Supply AB","Currency":"USD","TotalInvoiceCurrency":1890.00,"DueDate":"2026-05-10"},
	{"InvoiceNumber":"9001","SupplierNumber":"3","SupplierName":"Svensk Leverantor AB","Currency":"SEK","TotalInvoiceCurrency":5000.00,"DueDate":"2026-05-15"}
]}`

const connectorSupplierQuoted = `{"Supplier":{"SupplierNumber":"1","IBAN":"DE89370400440532013000","BIC":"COBADEFFXXX"}}`

// failingTokenStore reports a load error, exercising the Connector's error
// wrapping without reaching the network.
type failingTokenStore struct{ err error }

func (s failingTokenStore) Load(_ context.Context, _ domain.TenantID) (domain.OAuthToken, error) {
	return domain.OAuthToken{}, s.err
}

func (s failingTokenStore) Save(_ context.Context, _ domain.TenantID, _ domain.OAuthToken) error {
	return nil
}

func (s failingTokenStore) AtomicRefresh(_ context.Context, _ domain.TenantID, _, _ domain.OAuthToken) error {
	return nil
}

func (s failingTokenStore) Delete(_ context.Context, _ domain.TenantID) error { return nil }

// newFakeFortnox serves the supplier-invoice and supplier endpoints the
// Connector reads, recording the Authorization header it was called with so a
// test can prove the token loaded from the TokenStore reached the wire.
func newFakeFortnox(t *testing.T, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/3/supplierinvoices"):
			_, _ = io.WriteString(w, connectorInvoicesQuoted)
		case r.URL.Path == "/3/suppliers/1":
			_, _ = io.WriteString(w, connectorSupplierQuoted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newConnector builds the real adapter.Connector pointed at fortnoxURL via the
// config.Fortnox base-URL override seam (cobalt-dingo#30).
func newConnector(t *testing.T, fortnoxURL string, tokens domain.TokenStore) *adapterfortnox.Connector {
	t.Helper()
	cfg := config.Fortnox{
		Mode:            config.ModeProduction,
		BaseURLOverride: fortnoxURL,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return adapterfortnox.NewConnector(cfg, tokens, log)
}

// TestConnector_UnpaidInvoices_DecodesQuotedShapes drives the real Connector —
// token load, read-only client construction, transport and FlexInt decode —
// against a fake Fortnox. This is the coverage the #28 handler test could not
// reach while the base URL was hardcoded.
func TestConnector_UnpaidInvoices_DecodesQuotedShapes(t *testing.T) {
	var gotAuth string
	srv := newFakeFortnox(t, &gotAuth)
	conn := newConnector(t, srv.URL, newStubTokenStore())

	invoices, err := conn.UnpaidInvoices(context.Background(), domain.TenantID("acme"))

	require.NoError(t, err)
	require.Len(t, invoices, 3)

	assert.Equal(t, "Bearer test-access-token", gotAuth,
		"the token loaded from the TokenStore must reach the Fortnox request")

	// Quoted numerics decoded through FlexInt — the #26 regression class.
	assert.Equal(t, 1042, invoices[0].InvoiceNumber)
	assert.Equal(t, 1, invoices[0].SupplierNumber)
	assert.Equal(t, "Acme GmbH", invoices[0].SupplierName)
	assert.Equal(t, domain.MoneyFromFloat(2450.00, "EUR"), invoices[0].Amount)
	assert.Equal(t, "2026-05-03", invoices[0].DueDate)
	assert.True(t, invoices[0].IsForeignCurrency())

	// Bare numerics still decode, so the fix did not swap one shape for the other.
	assert.Equal(t, 1043, invoices[1].InvoiceNumber)
	assert.Equal(t, 2, invoices[1].SupplierNumber)

	// The Connector returns every row; FCY filtering belongs to the caller.
	assert.Equal(t, 9001, invoices[2].InvoiceNumber)
	assert.False(t, invoices[2].IsForeignCurrency())
}

// TestConnector_SupplierPaymentDetails_DecodesQuotedShapes covers the second
// domain port the Connector implements.
func TestConnector_SupplierPaymentDetails_DecodesQuotedShapes(t *testing.T) {
	var gotAuth string
	srv := newFakeFortnox(t, &gotAuth)
	conn := newConnector(t, srv.URL, newStubTokenStore())

	iban, bic, err := conn.SupplierPaymentDetails(context.Background(), domain.TenantID("acme"), 1)

	require.NoError(t, err)
	assert.Equal(t, "DE89370400440532013000", iban)
	assert.Equal(t, "COBADEFFXXX", bic)
	assert.Equal(t, "Bearer test-access-token", gotAuth)
}

// TestConnector_WrapsTokenLoadError pins the error-wrapping contract on both
// ports: a TokenStore failure surfaces as a wrapped connector error and never
// reaches Fortnox.
func TestConnector_WrapsTokenLoadError(t *testing.T) {
	sentinel := errors.New("token store unavailable")
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)

	conn := newConnector(t, srv.URL, failingTokenStore{err: sentinel})

	t.Run("UnpaidInvoices", func(t *testing.T) {
		_, err := conn.UnpaidInvoices(context.Background(), domain.TenantID("acme"))
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), "fortnox connector: load token:")
	})

	t.Run("SupplierPaymentDetails", func(t *testing.T) {
		_, _, err := conn.SupplierPaymentDetails(context.Background(), domain.TenantID("acme"), 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), "fortnox connector: load token:")
	})

	assert.False(t, called, "no Fortnox request may be made when the token cannot be loaded")
}

// TestConnector_UnpaidInvoices_WrapsUpstreamError pins the non-200 path, the
// shape a Fortnox outage or revoked token takes on the money path.
func TestConnector_UnpaidInvoices_WrapsUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	conn := newConnector(t, srv.URL, newStubTokenStore())

	_, err := conn.UnpaidInvoices(context.Background(), domain.TenantID("acme"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fortnox connector:")
	assert.Contains(t, err.Error(), "401")
}
