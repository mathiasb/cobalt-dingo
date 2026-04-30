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

func TestSupplierLedger_UnpaidInvoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.URL.Path, "/3/supplierinvoices")
		assert.Equal(t, "unpaid", r.URL.Query().Get("filter"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"SupplierInvoices": []map[string]any{
				{
					"InvoiceNumber":        42,
					"SupplierNumber":       7,
					"SupplierName":         "Acme GmbH",
					"Currency":             "EUR",
					"TotalInvoiceCurrency": 1000.50,
					"DueDate":              "2026-05-01",
				},
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewSupplierLedgerAdapter(srv.URL, newStubTokenStore(), false)
	invoices, err := adapter.UnpaidInvoices(context.Background(), "tenant-1")

	require.NoError(t, err)
	require.Len(t, invoices, 1)

	inv := invoices[0]
	assert.Equal(t, 42, inv.InvoiceNumber)
	assert.Equal(t, 7, inv.SupplierNumber)
	assert.Equal(t, "Acme GmbH", inv.SupplierName)
	assert.Equal(t, "EUR", inv.Amount.Currency)
	assert.Equal(t, "2026-05-01", inv.DueDate)
}

func TestSupplierLedger_InvoicePayments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.URL.Path, "/3/supplierinvoicepayments")
		assert.Equal(t, "42", r.URL.Query().Get("invoicenumber"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"SupplierInvoicePayments": []map[string]any{
				{
					"Number":         99,
					"InvoiceNumber":  42,
					"Amount":         1000.50,
					"AmountCurrency": 95.00,
					"Currency":       "EUR",
					"CurrencyRate":   10.53,
					"PaymentDate":    "2026-04-25",
					"Booked":         true,
				},
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewSupplierLedgerAdapter(srv.URL, newStubTokenStore(), false)
	payments, err := adapter.InvoicePayments(context.Background(), "tenant-1", 42)

	require.NoError(t, err)
	require.Len(t, payments, 1)

	p := payments[0]
	assert.Equal(t, 99, p.PaymentNumber)
	assert.Equal(t, 42, p.InvoiceNumber)
	assert.Equal(t, "EUR", p.Amount.Currency)
	assert.Equal(t, 10.53, p.CurrencyRate)
	assert.Equal(t, "2026-04-25", p.PaymentDate)
	assert.True(t, p.Booked)
}

func TestSupplierLedger_SupplierDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Contains(t, r.URL.Path, "/3/suppliers/7")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Supplier": map[string]any{
				"SupplierNumber": 7,
				"Name":           "Acme GmbH",
				"Email":          "billing@acme.de",
				"Phone1":         "+49 30 12345",
				"IBAN":           "DE89370400440532013000",
				"BIC":            "COBADEFFXXX",
				"Active":         true,
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewSupplierLedgerAdapter(srv.URL, newStubTokenStore(), false)
	supplier, err := adapter.SupplierDetail(context.Background(), "tenant-1", 7)

	require.NoError(t, err)
	assert.Equal(t, 7, supplier.SupplierNumber)
	assert.Equal(t, "Acme GmbH", supplier.Name)
	assert.Equal(t, "billing@acme.de", supplier.Email)
	assert.Equal(t, "+49 30 12345", supplier.Phone)
	assert.Equal(t, "DE89370400440532013000", supplier.IBAN)
	assert.Equal(t, "COBADEFFXXX", supplier.BIC)
	assert.True(t, supplier.Active)
}
