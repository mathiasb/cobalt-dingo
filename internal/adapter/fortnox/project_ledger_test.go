package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectLedger_Projects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/3/projects", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Projects": []map[string]any{
				{
					"ProjectNumber": "P001",
					"Description":   "Website Redesign",
					"Status":        "ONGOING",
					"StartDate":     "2025-01-01",
					"EndDate":       "",
				},
			},
		})
	}))
	defer srv.Close()

	gl := adapterfortnox.NewGeneralLedgerAdapter(srv.URL, newStubTokenStore(), false)
	adapter := adapterfortnox.NewProjectLedgerAdapter(srv.URL, newStubTokenStore(), gl, false)
	projects, err := adapter.Projects(context.Background(), "tenant-1")

	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "P001", projects[0].Number)
	assert.Equal(t, "Website Redesign", projects[0].Description)
	assert.Equal(t, "ONGOING", projects[0].Status)
}

func TestProjectLedger_ProjectTransactions(t *testing.T) {
	// Stub server handles both /3/financialyears and /3/vouchers
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/3/financialyears":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"FinancialYears": []map[string]any{
					{
						"Id":       1,
						"FromDate": "2025-01-01",
						"ToDate":   "2025-12-31",
					},
				},
			})
		case "/3/vouchers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Vouchers": []map[string]any{
					{
						"VoucherSeries":   "A",
						"VoucherNumber":   1,
						"Description":     "Project costs",
						"TransactionDate": "2025-03-15",
						"Year":            1,
						"VoucherRows": []map[string]any{
							{
								"Account":                1930,
								"Debit":                  0.0,
								"Credit":                 5000.0,
								"TransactionInformation": "Payment",
								"CostCenter":             "",
								"Project":                "P001",
							},
							{
								"Account":                5400,
								"Debit":                  5000.0,
								"Credit":                 0.0,
								"TransactionInformation": "Other cost",
								"CostCenter":             "",
								"Project":                "P002",
							},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gl := adapterfortnox.NewGeneralLedgerAdapter(srv.URL, newStubTokenStore(), false)
	adapter := adapterfortnox.NewProjectLedgerAdapter(srv.URL, newStubTokenStore(), gl, false)

	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)

	rows, err := adapter.ProjectTransactions(context.Background(), "tenant-1", "P001", from, to)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "P001", rows[0].Project)
	assert.Equal(t, 1930, rows[0].Account)
}
