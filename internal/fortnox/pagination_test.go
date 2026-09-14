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
