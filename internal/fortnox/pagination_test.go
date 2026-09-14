package fortnox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Found against live production on 2026-09-14: account 1930 reported "no
// activity, though the year holds 100 voucher(s)". Exactly 100 is Fortnox's
// page size — the year holds more, and every GL report was silently truncated
// to the first page. That silently wrong answer nearly deleted a whole phase of
// docs/live-financial-data.md.
//
// A truncated list is worse than an error: it is a plausible answer.
func TestListVouchers_followsEveryPage(t *testing.T) {
	var pagesRequested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		pagesRequested = append(pagesRequested, page)

		// Three pages, two vouchers each, so the total cannot be reached by
		// reading one page however large the page happens to be.
		body := fmt.Sprintf(`{"MetaInformation":{"@CurrentPage":%s,"@TotalPages":3,"@TotalResources":6},
		  "Vouchers":[{"VoucherSeries":"A","VoucherNumber":%s1,"TransactionDate":"2026-0%s-01"},
		              {"VoucherSeries":"A","VoucherNumber":%s2,"TransactionDate":"2026-0%s-02"}]}`,
			page, page, page, page, page)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).ListVouchers(3)
	require.NoError(t, err)

	assert.Len(t, got, 6, "every page must be read, not just the first")
	assert.Equal(t, []string{"1", "2", "3"}, pagesRequested,
		"pages are requested in order and stop at @TotalPages")
}

// A single page must not trigger a second request — the loop has to terminate
// on the metadata rather than on an empty response, or every call costs an
// extra round trip against a rate-limited API.
func TestListVouchers_singlePageMakesOneRequest(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":1},
		  "Vouchers":[{"VoucherSeries":"A","VoucherNumber":1,"TransactionDate":"2026-01-01"}]}`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).ListVouchers(3)
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, 1, calls)
}

// Absent metadata means one page, not zero and not infinite. Fortnox is
// documented to send it, but a client that loops on a missing field hangs.
func TestListVouchers_missingMetadataIsOnePage(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Vouchers":[{"VoucherSeries":"A","VoucherNumber":1,"TransactionDate":"2026-01-01"}]}`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).ListVouchers(3)
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, 1, calls, "no metadata must not mean keep going")
}

// Same defect, found the same way: account 1930 reported "not in the chart of
// accounts" for the company whose bank account it is. A BAS chart runs to
// several hundred accounts and ListAccounts read the first page.
//
// This is the second endpoint to produce a confident wrong answer from a
// truncated list in one session, which is why the audit of the rest is filed
// rather than left to be discovered one probe at a time.
func TestListAccounts_followsEveryPage(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		pages = append(pages, page)
		// Page 1 holds 1010; the account we care about is on page 2.
		body := `{"MetaInformation":{"@CurrentPage":1,"@TotalPages":2,"@TotalResources":2},
		  "Accounts":[{"Number":1010,"Description":"Utvecklingsutgifter","Active":true}]}`
		if page == "2" {
			body = `{"MetaInformation":{"@CurrentPage":2,"@TotalPages":2,"@TotalResources":2},
			  "Accounts":[{"Number":1930,"Description":"Företagskonto","Active":true,
			               "BalanceBroughtForward":1000.5,"BalanceCarriedForward":2500.75}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).ListAccounts(3)
	require.NoError(t, err)
	require.Len(t, got, 2, "an account on page two must still be found")

	var found bool
	for _, a := range got {
		if int(a.Number) == 1930 {
			found = true
			assert.InDelta(t, 2500.75, a.BalanceCarriedForward, 0.001)
		}
	}
	assert.True(t, found, "1930 was on page 2 and must be returned")
	assert.Equal(t, []string{"1", "2"}, pages)
}
