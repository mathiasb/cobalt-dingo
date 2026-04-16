package acceptance_test

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"time"

	"github.com/cucumber/godog"
	"github.com/mathiasb/cobalt-dingo/internal/invoice"
	"github.com/mathiasb/cobalt-dingo/internal/payment"
)

type paymentBatchCtx struct {
	invoices []invoice.EnrichedInvoice
	debtor   payment.Debtor
	xmlDoc   []byte
}

var pbCtx paymentBatchCtx

func enrichedFCYInvoicesReadyForPayment(table *godog.Table) error {
	pbCtx = paymentBatchCtx{}
	for _, row := range table.Rows[1:] {
		invNum, _ := strconv.Atoi(row.Cells[0].Value)
		supplierName := row.Cells[1].Value
		currency := row.Cells[2].Value
		total, _ := strconv.ParseFloat(row.Cells[3].Value, 64)
		dueDate := row.Cells[4].Value
		iban := row.Cells[5].Value
		bic := row.Cells[6].Value
		pbCtx.invoices = append(pbCtx.invoices, invoice.EnrichedInvoice{
			SupplierInvoice: invoice.SupplierInvoice{
				InvoiceNumber: invNum,
				SupplierName:  supplierName,
				Currency:      currency,
				Total:         total,
				DueDate:       dueDate,
			},
			IBAN: iban,
			BIC:  bic,
		})
	}
	return nil
}

func theDebtorIs(name, iban, bic string) error {
	pbCtx.debtor = payment.Debtor{Name: name, IBAN: iban, BIC: bic}
	return nil
}

func aPAIN001BatchIsGenerated() error {
	var err error
	pbCtx.xmlDoc, err = payment.GeneratePAIN001(
		pbCtx.invoices,
		pbCtx.debtor,
		"TEST-MSG-001",
		time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	)
	return err
}

func parsedDocument() (*payment.Document, error) {
	var doc payment.Document
	if err := xml.Unmarshal(pbCtx.xmlDoc, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal pain.001: %w", err)
	}
	return &doc, nil
}

func theDocumentContainsNPaymentTransactions(n int) error {
	doc, err := parsedDocument()
	if err != nil {
		return err
	}
	if doc.CstmrCdtTrfInitn.GrpHdr.NbOfTxs != n {
		return fmt.Errorf("expected NbOfTxs=%d, got %d", n, doc.CstmrCdtTrfInitn.GrpHdr.NbOfTxs)
	}
	return nil
}

func theDocumentControlSumIs(expected string) error {
	doc, err := parsedDocument()
	if err != nil {
		return err
	}
	got := doc.CstmrCdtTrfInitn.GrpHdr.CtrlSum
	if got != expected {
		return fmt.Errorf("expected CtrlSum=%q, got %q", expected, got)
	}
	return nil
}

func transactionHasCreditorIBANAndBIC(invNum int, iban, bic string) error {
	doc, err := parsedDocument()
	if err != nil {
		return err
	}
	endToEndID := fmt.Sprintf("INV-%d", invNum)
	for _, pmtInf := range doc.CstmrCdtTrfInitn.PmtInf {
		for _, tx := range pmtInf.CdtTrfTxInf {
			if tx.PmtID.EndToEndID != endToEndID {
				continue
			}
			if tx.CdtrAcct.ID.IBAN != iban {
				return fmt.Errorf("tx %s: expected IBAN %s, got %s", endToEndID, iban, tx.CdtrAcct.ID.IBAN)
			}
			if tx.CdtrAgt.FinInstnID.BIC != bic {
				return fmt.Errorf("tx %s: expected BIC %s, got %s", endToEndID, bic, tx.CdtrAgt.FinInstnID.BIC)
			}
			return nil
		}
	}
	return fmt.Errorf("transaction INV-%d not found in document", invNum)
}

func thereAreNPaymentInformationBlocks(n int) error {
	doc, err := parsedDocument()
	if err != nil {
		return err
	}
	got := len(doc.CstmrCdtTrfInitn.PmtInf)
	if got != n {
		return fmt.Errorf("expected %d PmtInf blocks, got %d", n, got)
	}
	return nil
}

func theCurrencyBlockContainsNTransactions(currency string, n int) error {
	doc, err := parsedDocument()
	if err != nil {
		return err
	}
	for _, pmtInf := range doc.CstmrCdtTrfInitn.PmtInf {
		// Identify the currency block by inspecting the first transaction's currency.
		if len(pmtInf.CdtTrfTxInf) == 0 {
			continue
		}
		if pmtInf.CdtTrfTxInf[0].Amt.InstdAmt.Ccy == currency {
			if len(pmtInf.CdtTrfTxInf) != n {
				return fmt.Errorf("%s block: expected %d transactions, got %d", currency, n, len(pmtInf.CdtTrfTxInf))
			}
			return nil
		}
	}
	return fmt.Errorf("no PmtInf block found for currency %s", currency)
}

func initializePaymentBatchSteps(sc *godog.ScenarioContext) {
	sc.Step(`^enriched FCY invoices ready for payment:$`, enrichedFCYInvoicesReadyForPayment)
	sc.Step(`^the debtor is "([^"]+)" with IBAN "([^"]+)" and BIC "([^"]+)"$`, theDebtorIs)
	sc.Step(`^a PAIN\.001 batch is generated$`, aPAIN001BatchIsGenerated)
	sc.Step(`^the document contains (\d+) payment transactions?$`, theDocumentContainsNPaymentTransactions)
	sc.Step(`^the document control sum is "([^"]+)"$`, theDocumentControlSumIs)
	sc.Step(`^transaction (\d+) has creditor IBAN "([^"]+)" and BIC "([^"]+)"$`, transactionHasCreditorIBANAndBIC)
	sc.Step(`^there are (\d+) payment information blocks$`, thereAreNPaymentInformationBlocks)
	sc.Step(`^the "([^"]+)" block contains (\d+) transactions?$`, theCurrencyBlockContainsNTransactions)
}
