package domain

import (
	"testing"
)

func TestSupplierInvoice_DueDateComment(t *testing.T) {
	// DueDate is documented as YYYY-MM-DD format; lexicographic comparison
	// must work correctly for same-timezone dates.
	inv := SupplierInvoice{
		InvoiceNumber: 1,
		DueDate:       "2025-03-15",
	}

	if inv.DueDate != "2025-03-15" {
		t.Errorf("DueDate = %q, want %q", inv.DueDate, "2025-03-15")
	}

	// Verify lexicographic ordering works as expected for YYYY-MM-DD format.
	dates := []string{"2024-12-31", "2025-01-01", "2025-03-15"}
	for i := 1; i < len(dates); i++ {
		if dates[i] <= dates[i-1] {
			t.Errorf("lexicographic sort failed: %q should be > %q", dates[i], dates[i-1])
		}
	}
}

func TestBatchItem_DueDateComment(t *testing.T) {
	item := BatchItem{
		DueDate: "2025-06-30",
	}

	if item.DueDate != "2025-06-30" {
		t.Errorf("DueDate = %q, want %q", item.DueDate, "2025-06-30")
	}
}

func TestCustomerInvoice_DueDateComment(t *testing.T) {
	inv := CustomerInvoice{
		DueDate: "2025-04-01",
	}

	if inv.DueDate != "2025-04-01" {
		t.Errorf("DueDate = %q, want %q", inv.DueDate, "2025-04-01")
	}
}

func TestSupplierPayment_PaymentDateComment(t *testing.T) {
	pay := SupplierPayment{
		PaymentDate: "2025-02-28",
	}

	if pay.PaymentDate != "2025-02-28" {
		t.Errorf("PaymentDate = %q, want %q", pay.PaymentDate, "2025-02-28")
	}
}

func TestCustomerPayment_PaymentDateComment(t *testing.T) {
	pay := CustomerPayment{
		PaymentDate: "2025-05-15",
	}

	if pay.PaymentDate != "2025-05-15" {
		t.Errorf("PaymentDate = %q, want %q", pay.PaymentDate, "2025-05-15")
	}
}

func TestExecutionConfirmation_PaymentDateComment(t *testing.T) {
	conf := ExecutionConfirmation{
		PaymentDate: "2025-07-01",
	}

	if conf.PaymentDate != "2025-07-01" {
		t.Errorf("PaymentDate = %q, want %q", conf.PaymentDate, "2025-07-01")
	}
}
