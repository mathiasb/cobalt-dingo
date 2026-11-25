package historyeval_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/historyeval"
	"github.com/stretchr/testify/assert"
)

// Errors out of the Fortnox client wrap URLs (which hold an invoice number) and
// the raw value that failed to decode (which can be an amount or a name). None
// of that may reach a Job log.
func TestSafeError_NeverReturnsTheTextOfTheUnderlyingErrorOnlyAClassOfFailure(t *testing.T) {
	sentinels := []string{"SENTINEL_NAME", "SENTINEL_INVNO", "SENTINEL_AMT", "556123-4567", "Definitely Mabe"}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"http status in a url", errors.New("get supplier invoice INV-SENTINEL_INVNO: GET http://x/3/supplierinvoices/INV-SENTINEL_INVNO: unexpected status 500"), "HTTP 500"},
		{"too many requests", errors.New("GET http://x/SENTINEL_NAME: unexpected status 429"), "HTTP 429"},
		{"an amount that failed to parse", errors.New(`decode supplier invoice INV-SENTINEL_INVNO: amount "SENTINEL_AMT_1.234,50" is not a number: strconv.ParseFloat`), "could not be decoded"},
		{"a field of the wrong shape", errors.New(`value is neither a string nor a number: {"n":"SENTINEL_NAME"}`), "could not be decoded"},
		{"json syntax", errors.New(`invalid character 'S' looking for beginning of value near SENTINEL_NAME`), "could not be decoded"},
		{"unmarshal", errors.New(`json: cannot unmarshal string into Go struct field .SupplierInvoices of type []x near SENTINEL_NAME`), "could not be decoded"},
		{"the company check on a mismatch", errors.New(`token opens "Definitely Mabe AB" (org 556123-4567), expected "SENTINEL_NAME"`), "different company"},
		{"a network failure", fmt.Errorf("GET http://SENTINEL_NAME/3/x: dial tcp: lookup SENTINEL_NAME: no such host"), "network"},
		{"something unclassified", errors.New("SENTINEL_NAME exploded with SENTINEL_AMT"), "withheld"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := historyeval.SafeError(tt.err)

			assert.Contains(t, got, tt.want)
			for _, s := range sentinels {
				assert.NotContains(t, got, s)
			}
			assert.False(t, strings.Contains(got, "http://"), "no url either")
		})
	}
}

func TestSafeError_OfNilIsEmpty(t *testing.T) {
	assert.Equal(t, "", historyeval.SafeError(nil))
}

// The classifier is only as good as its match against the REAL client's error
// text, so this drives the real Fortnox methods against a server that answers
// with hostile bodies and statuses and checks that nothing survives SafeError.
func TestSafeError_OfTheRealFortnoxClientsFailuresLeaksNothingFromTheServersAnswer(t *testing.T) {
	const sentinel = "SENTINEL_LEAK"
	cases := map[string]http.HandlerFunc{
		"a 500": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, sentinel, http.StatusInternalServerError) },
		"a 404": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, sentinel, http.StatusNotFound) },
		"an amount": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"SupplierInvoice":{"SupplierInvoiceRows":[{"Account":5460,"Debit":"`+sentinel+`"}]}}`)
		},
		"a name": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"SupplierInvoice":{"SupplierInvoiceRows":[{"Account":5460,"CostCenter":{"n":"`+sentinel+`"}}]}}`)
		},
		"not json": func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, sentinel) },
		"a wrong shape": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"SupplierInvoice":"`+sentinel+`"}`)
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			c := fortnox.NewClient(srv.URL, "tok", true)

			_, e1 := c.GetSupplierInvoiceRows("INV-" + sentinel)
			_, e2 := c.ListSupplierInvoiceHeads("2026-01-01", "2026-12-31")
			_, e3 := c.ListSupplierIdentities()

			for i, err := range []error{e1, e2, e3} {
				if err == nil {
					continue
				}
				got := historyeval.SafeError(err)
				assert.NotContains(t, got, sentinel, "call %d: %q", i, err)
				assert.NotContains(t, got, srv.URL, "call %d", i)
				assert.NotContains(t, got, "unclassified", "call %d: the real client's wording must be recognised: %q", i, err)
			}
		})
	}
}
