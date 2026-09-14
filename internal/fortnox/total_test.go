package fortnox

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TotalResources is the shared reader behind every "how many are there?"
// question. One implementation on purpose: the absent-vs-zero guard below was
// written once for vouchers, and a second hand-rolled copy is how it comes
// back (brain: a defect that keeps returning needs a shared code path).
func TestTotalResources_readsTheCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MetaInformation":{"@TotalResources":140},"SupplierInvoices":[]}`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).TotalResources(srv.URL + "/3/supplierinvoices?filter=fullypaid")
	require.NoError(t, err)
	assert.Equal(t, 140, got)
}

func TestTotalResources_zeroIsAValidCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MetaInformation":{"@TotalResources":0},"SupplierInvoices":[]}`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).TotalResources(srv.URL + "/3/supplierinvoices?filter=unpaid")
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

// The distinction #91 turns on. "No invoices matched this filter" and "this
// response carried no count" are different facts, and a zero that could mean
// either is worthless for deciding whether the company has open obligations.
func TestTotalResources_absentMetaInformationIsAnErrorNotZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"SupplierInvoices":[]}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok", true).TotalResources(srv.URL + "/3/supplierinvoices?filter=unpaid")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "metainformation")
}

// Every documented filter value, counted. A single "unpaid" number cannot
// distinguish a settled company from one whose invoices are all unbooked —
// and `unbooked` is a separate filter value in the spec, not a subset of
// `unpaid`.
func TestInvoiceStateCounts_countsEveryDocumentedFilter(t *testing.T) {
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path+"?"+r.URL.Query().Get("filter")]++
		_, _ = w.Write([]byte(`{"MetaInformation":{"@TotalResources":3}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", true)

	supplier, err := c.SupplierInvoiceStateCounts()
	require.NoError(t, err)
	for _, f := range SupplierInvoiceFilters {
		assert.Equal(t, 3, supplier[f], "filter %q not counted", f)
		assert.Equal(t, 1, seen["/3/supplierinvoices?"+f], "filter %q not requested exactly once", f)
	}

	customer, err := c.CustomerInvoiceStateCounts()
	require.NoError(t, err)
	for _, f := range CustomerInvoiceFilters {
		assert.Equal(t, 3, customer[f], "filter %q not counted", f)
		assert.Equal(t, 1, seen["/3/invoices?"+f], "filter %q not requested exactly once", f)
	}
}

// The filter lists must match the vendored spec. Fortnox documents
// pendingpayment and authorizepending for supplier invoices only, and omitting
// one hides a state the company can actually be in.
func TestInvoiceFilters_matchTheVendoredSpec(t *testing.T) {
	assert.ElementsMatch(t,
		[]string{"cancelled", "fullypaid", "unpaid", "unpaidoverdue", "unbooked", "pendingpayment", "authorizepending"},
		SupplierInvoiceFilters)
	assert.ElementsMatch(t,
		[]string{"cancelled", "fullypaid", "unpaid", "unpaidoverdue", "unbooked"},
		CustomerInvoiceFilters)
}

// A failed count must not become a zero in the map. Partial results here would
// understate obligations, which is the one direction that matters.
func TestInvoiceStateCounts_oneFailedCountFailsTheWhole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") == "unbooked" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"MetaInformation":{"@TotalResources":1}}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok", true).SupplierInvoiceStateCounts()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unbooked")
}
