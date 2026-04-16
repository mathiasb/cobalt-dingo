// Package payment assembles ISO 20022 PAIN.001 payment initiation documents.
package payment

import (
	"encoding/xml"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// Debtor identifies the entity initiating the payment (the cobalt-dingo tenant).
type Debtor struct {
	Name string
	IBAN string
	BIC  string
}

// Document is the root PAIN.001 XML element. Exported so callers can unmarshal
// generated documents for inspection and validation.
type Document struct {
	XMLName          xml.Name         `xml:"urn:iso:std:iso:20022:tech:xsd:pain.001.001.03 Document"`
	CstmrCdtTrfInitn CstmrCdtTrfInitn `xml:"CstmrCdtTrfInitn"`
}

// CstmrCdtTrfInitn is the Customer Credit Transfer Initiation element.
type CstmrCdtTrfInitn struct {
	GrpHdr GrpHdr   `xml:"GrpHdr"`
	PmtInf []PmtInf `xml:"PmtInf"`
}

// GrpHdr is the Group Header element.
type GrpHdr struct {
	MsgID    string `xml:"MsgId"`
	CreDtTm  string `xml:"CreDtTm"`
	NbOfTxs  int    `xml:"NbOfTxs"`
	CtrlSum  string `xml:"CtrlSum"`
	InitgPty Party  `xml:"InitgPty"`
}

// Party holds a party name (debtor or creditor).
type Party struct {
	Nm string `xml:"Nm"`
}

// PmtInf is one Payment Information block, grouping transactions by currency.
type PmtInf struct {
	PmtInfID    string        `xml:"PmtInfId"`
	PmtMtd      string        `xml:"PmtMtd"`
	NbOfTxs     int           `xml:"NbOfTxs"`
	CtrlSum     string        `xml:"CtrlSum"`
	PmtTpInf    *PmtTpInf     `xml:"PmtTpInf"`
	ReqdExctnDt string        `xml:"ReqdExctnDt"`
	Dbtr        Party         `xml:"Dbtr"`
	DbtrAcct    Account       `xml:"DbtrAcct"`
	DbtrAgt     Agent         `xml:"DbtrAgt"`
	CdtTrfTxInf []CdtTrfTxInf `xml:"CdtTrfTxInf"`
}

// PmtTpInf is Payment Type Information (optional; used for SEPA service level).
type PmtTpInf struct {
	SvcLvl SvcLvl `xml:"SvcLvl"`
}

// SvcLvl is the Service Level element.
type SvcLvl struct {
	Cd string `xml:"Cd"`
}

// Account wraps an IBAN account identifier.
type Account struct {
	ID AccountID `xml:"Id"`
}

// AccountID holds the IBAN.
type AccountID struct {
	IBAN string `xml:"IBAN"`
}

// Agent identifies a financial institution by BIC.
type Agent struct {
	FinInstnID FinancialInstitution `xml:"FinInstnId"`
}

// FinancialInstitution holds the BIC.
type FinancialInstitution struct {
	BIC string `xml:"BIC"`
}

// CdtTrfTxInf is one Credit Transfer Transaction (one per invoice).
type CdtTrfTxInf struct {
	PmtID    TxID    `xml:"PmtId"`
	Amt      Amt     `xml:"Amt"`
	CdtrAgt  Agent   `xml:"CdtrAgt"`
	Cdtr     Party   `xml:"Cdtr"`
	CdtrAcct Account `xml:"CdtrAcct"`
	RmtInf   RmtInf  `xml:"RmtInf"`
}

// TxID carries the end-to-end identifier for the transaction.
type TxID struct {
	EndToEndID string `xml:"EndToEndId"`
}

// Amt wraps the instructed amount.
type Amt struct {
	InstdAmt InstructedAmt `xml:"InstdAmt"`
}

// InstructedAmt is the amount with its ISO 4217 currency code.
type InstructedAmt struct {
	Ccy   string `xml:"Ccy,attr"`
	Value string `xml:",chardata"`
}

// RmtInf carries unstructured remittance information (invoice reference).
type RmtInf struct {
	Ustrd string `xml:"Ustrd"`
}

// GeneratePAIN001 assembles a pain.001.001.03 XML document from enriched invoices.
// Invoices are grouped by currency into separate PmtInf blocks; EUR blocks carry
// a SEPA service level. msgID must be unique per submission (max 35 chars).
// Production callers must ensure ReqdExctnDt is a valid future banking day.
func GeneratePAIN001(invoices []domain.EnrichedInvoice, debtor Debtor, msgID string, createdAt time.Time) ([]byte, error) {
	if len(invoices) == 0 {
		return nil, fmt.Errorf("generate pain.001: no invoices provided")
	}

	// Group by currency, preserving first-seen order for deterministic output.
	order := []string{}
	grouped := map[string][]domain.EnrichedInvoice{}
	for _, inv := range invoices {
		if _, seen := grouped[inv.Amount.Currency]; !seen {
			order = append(order, inv.Amount.Currency)
		}
		grouped[inv.Amount.Currency] = append(grouped[inv.Amount.Currency], inv)
	}

	pmtInfs := make([]PmtInf, 0, len(order))
	for i, ccy := range order {
		pmtInfs = append(pmtInfs, buildPmtInf(i+1, ccy, grouped[ccy], debtor))
	}

	doc := Document{
		CstmrCdtTrfInitn: CstmrCdtTrfInitn{
			GrpHdr: GrpHdr{
				MsgID:    msgID,
				CreDtTm:  createdAt.UTC().Format(time.RFC3339),
				NbOfTxs:  len(invoices),
				CtrlSum:  fmtAmt(sumAmounts(invoices)),
				InitgPty: Party{Nm: debtor.Name},
			},
			PmtInf: pmtInfs,
		},
	}

	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal pain.001: %w", err)
	}
	return append([]byte(xml.Header), out...), nil
}

func buildPmtInf(idx int, ccy string, invoices []domain.EnrichedInvoice, debtor Debtor) PmtInf {
	txs := make([]CdtTrfTxInf, len(invoices))
	for i, inv := range invoices {
		txs[i] = CdtTrfTxInf{
			PmtID:    TxID{EndToEndID: fmt.Sprintf("INV-%d", inv.InvoiceNumber)},
			Amt:      Amt{InstdAmt: InstructedAmt{Ccy: inv.Amount.Currency, Value: fmtAmt(inv.Amount.Float())}},
			CdtrAgt:  Agent{FinInstnID: FinancialInstitution{BIC: inv.BIC}},
			Cdtr:     Party{Nm: inv.SupplierName},
			CdtrAcct: Account{ID: AccountID{IBAN: inv.IBAN}},
			RmtInf:   RmtInf{Ustrd: fmt.Sprintf("Invoice %d", inv.InvoiceNumber)},
		}
	}

	pi := PmtInf{
		PmtInfID:    fmt.Sprintf("PMINF-%02d", idx),
		PmtMtd:      "TRF",
		NbOfTxs:     len(invoices),
		CtrlSum:     fmtAmt(sumAmounts(invoices)),
		ReqdExctnDt: invoices[0].DueDate, // caller must ensure this is a future banking day
		Dbtr:        Party{Nm: debtor.Name},
		DbtrAcct:    Account{ID: AccountID{IBAN: debtor.IBAN}},
		DbtrAgt:     Agent{FinInstnID: FinancialInstitution{BIC: debtor.BIC}},
		CdtTrfTxInf: txs,
	}

	// SEPA service level applies to EUR transfers.
	if ccy == "EUR" {
		pi.PmtTpInf = &PmtTpInf{SvcLvl: SvcLvl{Cd: "SEPA"}}
	}

	return pi
}

func sumAmounts(invoices []domain.EnrichedInvoice) float64 {
	var sum float64
	for _, inv := range invoices {
		sum += inv.Amount.Float()
	}
	return sum
}

func fmtAmt(v float64) string {
	return fmt.Sprintf("%.2f", v)
}
