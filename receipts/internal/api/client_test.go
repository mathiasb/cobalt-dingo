package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mathiasb/coo-agent/internal/api"
	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/mathiasb/coo-agent/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ListInvoices ---

func TestClient_ListInvoices_ReturnsInvoices(t *testing.T) {
	srv := newFakeFortnox(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/3/invoices", r.URL.Path)
		assertBearerToken(t, r)
		writeJSON(w, map[string]any{
			"Invoices": []map[string]any{
				{"DocumentNumber": "1001", "CustomerName": "Acme AB", "Total": 12500.0, "Balance": 12500.0},
			},
		})
	})

	client := newTestClient(t, srv.URL)
	invoices, err := client.ListInvoices(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, invoices, 1)
	assert.Equal(t, "1001", invoices[0].DocumentNumber)
	assert.Equal(t, "Acme AB", invoices[0].CustomerName)
	assert.Equal(t, 12500.0, invoices[0].Total)
}

func TestClient_ListInvoices_ForwardsFilterParameter(t *testing.T) {
	srv := newFakeFortnox(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "unpaid", r.URL.Query().Get("filter"))
		writeJSON(w, map[string]any{"Invoices": []any{}})
	})

	client := newTestClient(t, srv.URL)
	_, err := client.ListInvoices(context.Background(), "unpaid")
	require.NoError(t, err)
}

func TestClient_ListInvoices_LogsAuditEntry(t *testing.T) {
	srv := newFakeFortnox(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"Invoices": []any{}})
	})

	auditLog := &audit.MemoryLogger{}
	client := newTestClientWithAudit(t, srv.URL, auditLog)

	_, err := client.ListInvoices(context.Background(), "")
	require.NoError(t, err)

	entries := auditLog.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "GET", entries[0].Op)
	assert.Contains(t, entries[0].Endpoint, "invoices")
	assert.Equal(t, http.StatusOK, entries[0].Status)
	assert.GreaterOrEqual(t, entries[0].DurationMs, int64(0))
}

// --- Retry behaviour ---

func TestClient_RetriesOnHTTP429(t *testing.T) {
	attempts := 0
	srv := newFakeFortnox(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(w, map[string]any{"Invoices": []any{}})
	})

	client := newTestClient(t, srv.URL)
	_, err := client.ListInvoices(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, 3, attempts, "client should retry on 429 and succeed on third attempt")
}

func TestClient_StopsRetryingAfterMaxAttempts(t *testing.T) {
	srv := newFakeFortnox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	client := newTestClient(t, srv.URL)
	_, err := client.ListInvoices(context.Background(), "")
	assert.Error(t, err, "client should return an error after exhausting retries")
}

func TestClient_DoesNotRetryOnHTTP400(t *testing.T) {
	attempts := 0
	srv := newFakeFortnox(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ErrorInformation":{"Error":1,"Message":"Bad request"}}`))
	})

	client := newTestClient(t, srv.URL)
	_, err := client.ListInvoices(context.Background(), "")
	assert.Error(t, err)
	assert.Equal(t, 1, attempts, "client must not retry on 4xx client errors")
}

// --- CreateVoucher ---

func TestClient_CreateVoucher_SendsCorrectPayload(t *testing.T) {
	var received map[string]any
	srv := newFakeFortnox(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/3/vouchers", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]any{
			"Voucher": map[string]any{
				"VoucherNumber": 42,
				"Description":   "Telenor mars 2025",
				"VoucherDate":   "2025-03-31",
				"VoucherRows":   received["Voucher"].(map[string]any)["VoucherRows"],
			},
		})
	})

	client := newTestClient(t, srv.URL)
	voucher := api.Voucher{
		Description: "Telenor mars 2025",
		VoucherDate: "2025-03-31",
		Rows: []api.VoucherRow{
			{Account: 6212, Debit: 360},
			{Account: 2640, Debit: 90},
			{Account: 2893, Credit: 450},
		},
	}
	result, err := client.CreateVoucher(context.Background(), voucher)
	require.NoError(t, err)
	assert.Equal(t, 42, result.VoucherNumber)
}

func TestClient_CreateVoucher_LogsAuditEntry(t *testing.T) {
	srv := newFakeFortnox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]any{"Voucher": map[string]any{"VoucherNumber": 1}})
	})

	auditLog := &audit.MemoryLogger{}
	client := newTestClientWithAudit(t, srv.URL, auditLog)
	_, err := client.CreateVoucher(context.Background(), api.Voucher{Description: "test", VoucherDate: "2025-01-01"})
	require.NoError(t, err)

	entries := auditLog.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "POST", entries[0].Op)
	assert.Equal(t, http.StatusCreated, entries[0].Status)
}

// --- Token refresh ---

func TestClient_RefreshesTokenWhenExpired(t *testing.T) {
	refreshed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			refreshed = true
			writeJSON(w, map[string]any{
				"access_token":  "new-access-token",
				"refresh_token": "new-refresh-token",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
		default:
			// After refresh, the new token should be used.
			assert.Equal(t, "Bearer new-access-token", r.Header.Get("Authorization"))
			writeJSON(w, map[string]any{"Invoices": []any{}})
		}
	}))
	t.Cleanup(srv.Close)

	expiredToken := &auth.Token{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(-time.Hour), // already expired
	}
	tokenStore := &auth.MemoryTokenStore{}
	require.NoError(t, tokenStore.Save(expiredToken))

	client, err := api.New(api.Config{
		ClientID:        "test-id",
		ClientSecret:    "test-secret",
		BaseURL:         srv.URL + "/3/",
		TokenRefreshURL: srv.URL + "/oauth/token",
		TokenStore:      tokenStore,
		AuditLog:        &audit.MemoryLogger{},
	})
	require.NoError(t, err)

	_, err = client.ListInvoices(context.Background(), "")
	require.NoError(t, err)
	assert.True(t, refreshed, "client must refresh an expired token before making requests")

	// The new token must also be persisted so subsequent calls use it.
	saved, err := tokenStore.Load()
	require.NoError(t, err)
	assert.Equal(t, "new-access-token", saved.AccessToken, "refreshed token must be persisted")
}

// --- helpers ---

func newFakeFortnox(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newTestClient(t *testing.T, baseURL string) *api.Client {
	t.Helper()
	return newTestClientWithAudit(t, baseURL, &audit.MemoryLogger{})
}

func newTestClientWithAudit(t *testing.T, baseURL string, auditLog audit.Logger) *api.Client {
	t.Helper()
	token := &auth.Token{
		AccessToken: "test-bearer-token",
		Expiry:      time.Now().Add(time.Hour),
	}
	tokenStore := &auth.MemoryTokenStore{}
	require.NoError(t, tokenStore.Save(token))

	client, err := api.New(api.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		BaseURL:      baseURL + "/3/",
		TokenStore:   tokenStore,
		AuditLog:     auditLog,
	})
	require.NoError(t, err)
	return client
}

func assertBearerToken(t *testing.T, r *http.Request) {
	t.Helper()
	assert.Equal(t, "Bearer test-bearer-token", r.Header.Get("Authorization"))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
