// Package fake provides stub implementations of domain ports for development
// and testing without external dependencies.
package fake

import (
	"context"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// InvoiceSource returns a fixed set of foreign-currency invoices.
// Implements domain.InvoiceSource.
type InvoiceSource struct{}

var fakeInvoices = []domain.SupplierInvoice{
	{InvoiceNumber: 1042, SupplierNumber: 1, SupplierName: "Acme GmbH", Amount: domain.MoneyFromFloat(2450.00, "EUR"), DueDate: "2026-05-03"},
	{InvoiceNumber: 1043, SupplierNumber: 2, SupplierName: "Nordic Supply AB", Amount: domain.MoneyFromFloat(1890.00, "USD"), DueDate: "2026-05-10"},
	{InvoiceNumber: 1044, SupplierNumber: 3, SupplierName: "London Parts Ltd", Amount: domain.MoneyFromFloat(3200.00, "GBP"), DueDate: "2026-04-14"},
	{InvoiceNumber: 1045, SupplierNumber: 4, SupplierName: "Swiss Precision SA", Amount: domain.MoneyFromFloat(890.00, "CHF"), DueDate: "2026-05-20"},
}

// UnpaidInvoices implements domain.InvoiceSource.
func (InvoiceSource) UnpaidInvoices(_ context.Context, _ domain.TenantID) ([]domain.SupplierInvoice, error) {
	return fakeInvoices, nil
}

// SupplierEnricher returns hardcoded IBAN/BIC for the fake invoice suppliers.
// Implements domain.SupplierEnricher.
type SupplierEnricher struct{}

var fakePaymentDetails = map[int][2]string{
	1: {"DE89370400440532013000", "COBADEFFXXX"},
	2: {"GB29NWBK60161331926819", "NWBKGB2L"},
	3: {"GB29NWBK60161331926820", "NWBKGB2L"},
	4: {"CH9300762011623852957", "UBSWCHZH"},
}

// SupplierPaymentDetails implements domain.SupplierEnricher.
func (SupplierEnricher) SupplierPaymentDetails(_ context.Context, _ domain.TenantID, supplierNumber int) (string, string, error) {
	if details, ok := fakePaymentDetails[supplierNumber]; ok {
		return details[0], details[1], nil
	}
	return "", "", nil
}
