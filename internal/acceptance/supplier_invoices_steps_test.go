package acceptance_test

import (
	"fmt"
	"strconv"

	"github.com/cucumber/godog"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// supplierInvoicesCtx holds scenario-scoped state for supplier invoice steps.
type supplierInvoicesCtx struct {
	fortnoxInvoices []domain.SupplierInvoice
	queue           domain.Queue
}

var siCtx supplierInvoicesCtx

func fortnoxHasTheFollowingUnpaidSupplierInvoices(table *godog.Table) error {
	siCtx = supplierInvoicesCtx{} // reset per scenario
	for _, row := range table.Rows[1:] {
		num, err := strconv.Atoi(row.Cells[0].Value)
		if err != nil {
			return fmt.Errorf("parse InvoiceNumber: %w", err)
		}
		total, err := strconv.ParseFloat(row.Cells[2].Value, 64)
		if err != nil {
			return fmt.Errorf("parse Total: %w", err)
		}
		siCtx.fortnoxInvoices = append(siCtx.fortnoxInvoices, domain.SupplierInvoice{
			InvoiceNumber: num,
			Amount:        domain.MoneyFromFloat(total, row.Cells[1].Value),
			DueDate:       row.Cells[3].Value,
		})
	}
	return nil
}

func theInvoiceSyncRuns() error {
	domain.Sync(siCtx.fortnoxInvoices, &siCtx.queue)
	return nil
}

func thePaymentQueueContainsInvoicesAnd(a, b int) error {
	found := map[int]bool{}
	for _, inv := range siCtx.queue.All() {
		found[inv.InvoiceNumber] = true
	}
	if !found[a] {
		return fmt.Errorf("expected invoice %d in payment queue, not found", a)
	}
	if !found[b] {
		return fmt.Errorf("expected invoice %d in payment queue, not found", b)
	}
	return nil
}

func thePaymentQueueDoesNotContainInvoice(num int) error {
	for _, inv := range siCtx.queue.All() {
		if inv.InvoiceNumber == num {
			return fmt.Errorf("invoice %d should not be in payment queue, but it is", num)
		}
	}
	return nil
}

func initializeSupplierInvoiceSteps(sc *godog.ScenarioContext) {
	sc.Step(`^Fortnox has the following unpaid supplier invoices:$`, fortnoxHasTheFollowingUnpaidSupplierInvoices)
	sc.Step(`^the invoice sync runs$`, theInvoiceSyncRuns)
	sc.Step(`^the payment queue contains invoices (\d+) and (\d+)$`, thePaymentQueueContainsInvoicesAnd)
	sc.Step(`^the payment queue does not contain invoice (\d+)$`, thePaymentQueueDoesNotContainInvoice)
}
