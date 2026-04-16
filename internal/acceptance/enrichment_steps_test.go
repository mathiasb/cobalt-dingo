package acceptance_test

import (
	"fmt"
	"strconv"

	"github.com/cucumber/godog"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

type enrichmentCtx struct {
	invoices        []domain.SupplierInvoice
	supplierDetails map[int][2]string // supplierNumber → [IBAN, BIC]
	enriched        []domain.EnrichedInvoice
	skipped         []domain.SupplierInvoice
}

var enCtx enrichmentCtx

func fcyInvoicesForSuppliers(table *godog.Table) error {
	enCtx = enrichmentCtx{supplierDetails: map[int][2]string{}}
	for _, row := range table.Rows[1:] {
		invNum, _ := strconv.Atoi(row.Cells[0].Value)
		supNum, _ := strconv.Atoi(row.Cells[1].Value)
		total, _ := strconv.ParseFloat(row.Cells[3].Value, 64)
		enCtx.invoices = append(enCtx.invoices, domain.SupplierInvoice{
			InvoiceNumber:  invNum,
			SupplierNumber: supNum,
			Amount:         domain.MoneyFromFloat(total, row.Cells[2].Value),
			DueDate:        row.Cells[4].Value,
		})
	}
	return nil
}

func theFortnoxSupplierAPIReturns(table *godog.Table) error {
	for _, row := range table.Rows[1:] {
		supNum, _ := strconv.Atoi(row.Cells[0].Value)
		enCtx.supplierDetails[supNum] = [2]string{row.Cells[1].Value, row.Cells[2].Value}
	}
	return nil
}

func ibanBICEnrichmentRuns() error {
	lookup := func(supplierNumber int) (string, string, error) {
		details, ok := enCtx.supplierDetails[supplierNumber]
		if !ok {
			return "", "", fmt.Errorf("supplier %d not found in stub", supplierNumber)
		}
		return details[0], details[1], nil
	}

	// Track skipped invoices separately for scenario 2 assertions.
	for _, inv := range enCtx.invoices {
		iban, _, _ := lookup(inv.SupplierNumber)
		if iban == "" {
			enCtx.skipped = append(enCtx.skipped, inv)
		}
	}

	var err error
	enCtx.enriched, err = domain.Enrich(enCtx.invoices, lookup)
	return err
}

func invoiceHasIBANAndBIC(invNum int, iban, bic string) error {
	for _, e := range enCtx.enriched {
		if e.InvoiceNumber == invNum {
			if e.IBAN != iban {
				return fmt.Errorf("invoice %d: expected IBAN %s, got %s", invNum, iban, e.IBAN)
			}
			if e.BIC != bic {
				return fmt.Errorf("invoice %d: expected BIC %s, got %s", invNum, bic, e.BIC)
			}
			return nil
		}
	}
	return fmt.Errorf("invoice %d not found in enriched results", invNum)
}

func nInvoiceIsReadyForPayment(n int) error {
	if len(enCtx.enriched) != n {
		return fmt.Errorf("expected %d invoices ready, got %d", n, len(enCtx.enriched))
	}
	return nil
}

func invoiceIsReadyWithIBAN(invNum int, iban string) error {
	for _, e := range enCtx.enriched {
		if e.InvoiceNumber == invNum {
			if e.IBAN != iban {
				return fmt.Errorf("invoice %d: expected IBAN %s, got %s", invNum, iban, e.IBAN)
			}
			return nil
		}
	}
	return fmt.Errorf("invoice %d not found in enriched results", invNum)
}

func invoiceIsSkippedDueToMissingIBAN(invNum int) error {
	for _, inv := range enCtx.skipped {
		if inv.InvoiceNumber == invNum {
			return nil
		}
	}
	return fmt.Errorf("expected invoice %d to be skipped, but it was not", invNum)
}

func initializeEnrichmentSteps(sc *godog.ScenarioContext) {
	sc.Step(`^FCY invoices for suppliers \d+ and \d+:$`, fcyInvoicesForSuppliers)
	sc.Step(`^the Fortnox supplier API returns:$`, theFortnoxSupplierAPIReturns)
	sc.Step(`^IBAN/BIC enrichment runs$`, ibanBICEnrichmentRuns)
	sc.Step(`^invoice (\d+) has IBAN (\S+) and BIC (\S+)$`, invoiceHasIBANAndBIC)
	sc.Step(`^(\d+) invoice is ready for payment$`, nInvoiceIsReadyForPayment)
	sc.Step(`^invoice (\d+) is ready with IBAN (\S+)$`, invoiceIsReadyWithIBAN)
	sc.Step(`^invoice (\d+) is skipped due to missing IBAN$`, invoiceIsSkippedDueToMissingIBAN)
}
