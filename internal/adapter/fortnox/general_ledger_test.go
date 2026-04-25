package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneralLedger_ChartOfAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/3/accounts", r.URL.Path)
		assert.Equal(t, "1", r.URL.Query().Get("financialyear"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Accounts": []map[string]any{
				{
					"Number":                1930,
					"Description":           "Företagskonto",
					"SRU":                   0,
					"Active":                true,
					"BalanceBroughtForward": 50000.0,
					"BalanceCarriedForward": 55000.0,
				},
				{
					"Number":                2440,
					"Description":           "Leverantörsskulder",
					"SRU":                   0,
					"Active":                true,
					"BalanceBroughtForward": -10000.0,
					"BalanceCarriedForward": -8000.0,
				},
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewGeneralLedgerAdapter(srv.URL, newStubTokenStore())
	accounts, err := adapter.ChartOfAccounts(context.Background(), "tenant-1", 1)

	require.NoError(t, err)
	require.Len(t, accounts, 2)
	assert.Equal(t, 1930, accounts[0].Number)
	assert.Equal(t, "Företagskonto", accounts[0].Description)
	assert.Equal(t, "SEK", accounts[0].BalanceBF.Currency)
}

func TestGeneralLedger_FinancialYears(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/3/financialyears", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"FinancialYears": []map[string]any{
				{
					"Id":       1,
					"FromDate": "2025-01-01",
					"ToDate":   "2025-12-31",
				},
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewGeneralLedgerAdapter(srv.URL, newStubTokenStore())
	years, err := adapter.FinancialYears(context.Background(), "tenant-1")

	require.NoError(t, err)
	require.Len(t, years, 1)
	assert.Equal(t, 1, years[0].ID)
	assert.Equal(t, 2025, years[0].From.Year())
	assert.Equal(t, 2025, years[0].To.Year())
}
