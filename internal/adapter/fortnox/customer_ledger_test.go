package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

func TestCustomerLedger_UnpaidInvoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/3/invoices", r.URL.Path)
		assert.Equal(t, "unpaid", r.URL.Query().Get("filter"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Invoices": []map[string]any{
				{
					"DocumentNumber": 2001,
					"CustomerNumber": "10",
					"CustomerName":   "Test AB",
					"Currency":       "SEK",
					"Total":          1500.0,
					"Balance":        750.0,
					"DueDate":        "2026-05-01",
					"InvoiceDate":    "2026-04-01",
					"Booked":         true,
					"Cancelled":      false,
					"Sent":           true,
				},
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewCustomerLedgerAdapter(srv.URL, &stubTokenStore{})
	invoices, err := adapter.UnpaidInvoices(context.Background(), domain.TenantID("t1"))

	require.NoError(t, err)
	require.Len(t, invoices, 1)

	inv := invoices[0]
	assert.Equal(t, 2001, inv.InvoiceNumber)
	assert.Equal(t, 10, inv.CustomerNumber)
	assert.Equal(t, "Test AB", inv.CustomerName)
	assert.Equal(t, domain.MoneyFromFloat(1500.0, "SEK"), inv.Amount)
	assert.Equal(t, domain.MoneyFromFloat(750.0, "SEK"), inv.Balance)
	assert.Equal(t, "2026-05-01", inv.DueDate)
	assert.Equal(t, "2026-04-01", inv.InvoiceDate)
	assert.True(t, inv.Booked)
	assert.False(t, inv.Cancelled)
	assert.True(t, inv.Sent)
}

func TestCustomerLedger_InvoicePayments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/3/invoicepayments", r.URL.Path)
		assert.Equal(t, "2001", r.URL.Query().Get("invoicenumber"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"InvoicePayments": []map[string]any{
				{
					"Number":         42,
					"InvoiceNumber":  2001,
					"Amount":         750.0,
					"AmountCurrency": 750.0,
					"Currency":       "SEK",
					"PaymentDate":    "2026-04-15",
					"Booked":         true,
				},
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewCustomerLedgerAdapter(srv.URL, &stubTokenStore{})
	payments, err := adapter.InvoicePayments(context.Background(), domain.TenantID("t1"), 2001)

	require.NoError(t, err)
	require.Len(t, payments, 1)

	p := payments[0]
	assert.Equal(t, 42, p.PaymentNumber)
	assert.Equal(t, 2001, p.InvoiceNumber)
	assert.Equal(t, domain.MoneyFromFloat(750.0, "SEK"), p.Amount)
	assert.Equal(t, "2026-04-15", p.PaymentDate)
	assert.True(t, p.Booked)
}

func TestCustomerLedger_CustomerDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/3/customers/10", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Customer": map[string]any{
				"CustomerNumber": "10",
				"Name":           "Test AB",
				"Email":          "test@example.com",
				"Phone1":         "0701234567",
				"Active":         true,
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewCustomerLedgerAdapter(srv.URL, &stubTokenStore{})
	customer, err := adapter.CustomerDetail(context.Background(), domain.TenantID("t1"), 10)

	require.NoError(t, err)
	assert.Equal(t, 10, customer.CustomerNumber)
	assert.Equal(t, "Test AB", customer.Name)
	assert.Equal(t, "test@example.com", customer.Email)
	assert.Equal(t, "0701234567", customer.Phone)
	assert.True(t, customer.Active)
}
