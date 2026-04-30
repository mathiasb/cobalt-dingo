package fortnox_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCostCenterLedger_CostCenters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/3/costcenters", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"CostCenters": []map[string]any{
				{"Code": "100", "Description": "Sales", "Active": true},
				{"Code": "200", "Description": "Engineering", "Active": true},
			},
		})
	}))
	defer srv.Close()

	gl := adapterfortnox.NewGeneralLedgerAdapter(srv.URL, newStubTokenStore(), false)
	adapter := adapterfortnox.NewCostCenterLedgerAdapter(srv.URL, newStubTokenStore(), gl, false)
	centers, err := adapter.CostCenters(context.Background(), "tenant-1")

	require.NoError(t, err)
	require.Len(t, centers, 2)
	assert.Equal(t, "100", centers[0].Code)
	assert.Equal(t, "Sales", centers[0].Description)
	assert.True(t, centers[0].Active)
	assert.Equal(t, "200", centers[1].Code)
	assert.Equal(t, "Engineering", centers[1].Description)
}

func TestCostCenterLedger_CostCenterTransactions(t *testing.T) {
	// The adapter calls FinancialYears first, then Vouchers for the matching year.
	// We serve all three endpoints from a single mux.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/financialyears":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"FinancialYears": []map[string]any{
					{"Id": 1, "FromDate": "2025-01-01", "ToDate": "2025-12-31"},
				},
			})
		case "/3/vouchers":
			assert.Equal(t, "1", r.URL.Query().Get("financialyear"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Vouchers": []map[string]any{
					{
						"VoucherSeries":   "A",
						"VoucherNumber":   1,
						"Description":     "Invoice",
						"TransactionDate": "2025-03-15",
						"Year":            1,
						"VoucherRows": []map[string]any{
							{"Account": 4010, "Debit": 1000.0, "Credit": 0.0, "CostCenter": "100", "Project": ""},
							{"Account": 2440, "Debit": 0.0, "Credit": 1000.0, "CostCenter": "200", "Project": ""},
						},
					},
					{
						"VoucherSeries":   "B",
						"VoucherNumber":   2,
						"Description":     "Other",
						"TransactionDate": "2025-06-01",
						"Year":            1,
						"VoucherRows": []map[string]any{
							{"Account": 5010, "Debit": 500.0, "Credit": 0.0, "CostCenter": "100", "Project": ""},
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error": "not found: %s"}`, r.URL.Path)
		}
	}))
	defer srv.Close()

	gl := adapterfortnox.NewGeneralLedgerAdapter(srv.URL, newStubTokenStore(), false)
	adapter := adapterfortnox.NewCostCenterLedgerAdapter(srv.URL, newStubTokenStore(), gl, false)

	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	rows, err := adapter.CostCenterTransactions(context.Background(), "tenant-1", "100", from, to)

	require.NoError(t, err)
	// Only rows with CostCenter == "100" should be returned (2 rows across 2 vouchers).
	require.Len(t, rows, 2)
	assert.Equal(t, 4010, rows[0].Account)
	assert.Equal(t, "100", rows[0].CostCenter)
	assert.Equal(t, 5010, rows[1].Account)
	assert.Equal(t, "100", rows[1].CostCenter)
}
