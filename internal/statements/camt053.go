package statements

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// Camt053 parses ISO 20022 camt.053 bank-to-customer statements.
//
// The XML element names below carry no namespace, which is deliberate:
// encoding/xml then matches on local name and the same parser handles
// camt.053.001.02 through .001.08 without a version table. Banks differ on
// which minor version they emit, and several emit more than one over time.
type Camt053 struct{}

// Parse implements Parser.
func (Camt053) Parse(r io.Reader, sourceRef string) (Statement, error) {
	return ParseCamt053(r, sourceRef)
}

// ParseCamt053 reads a camt.053 document and returns its first statement.
//
// Only entries with status BOOK are returned. Pending entries (PDNG) carry a
// real amount but have not hit the account, so including them breaks the
// balance identity while looking entirely correct — see CheckBalance.
func ParseCamt053(r io.Reader, sourceRef string) (Statement, error) {
	var doc camtDocument
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return Statement{}, fmt.Errorf("decode camt.053: %w", err)
	}
	if len(doc.Statements) == 0 {
		return Statement{}, fmt.Errorf("camt.053 %q contains no <Stmt> element", sourceRef)
	}
	if len(doc.Statements) > 1 {
		// Multi-statement files exist (several accounts in one delivery). Refuse
		// rather than silently returning the first: picking one at random is how
		// an account's transactions go missing without an error.
		return Statement{}, fmt.Errorf(
			"camt.053 %q contains %d statements; multi-account files are not supported yet",
			sourceRef, len(doc.Statements))
	}

	raw := doc.Statements[0]
	account := strings.TrimSpace(raw.Account.ID.IBAN)
	if account == "" {
		account = strings.TrimSpace(raw.Account.ID.Othr.ID)
	}

	st := Statement{
		ID:        strings.TrimSpace(raw.ID),
		Account:   account,
		Source:    SourceCamt053,
		SourceRef: sourceRef,
	}

	var err error
	if st.From, err = parseCamtTime(raw.Period.From); err != nil {
		return Statement{}, fmt.Errorf("statement period start: %w", err)
	}
	if st.To, err = parseCamtTime(raw.Period.To); err != nil {
		return Statement{}, fmt.Errorf("statement period end: %w", err)
	}

	if st.OpeningBalance, err = balanceByCode(raw.Balances, "OPBD", "PRCD"); err != nil {
		return Statement{}, fmt.Errorf("statement %q: %w", st.ID, err)
	}
	if st.ClosingBalance, err = balanceByCode(raw.Balances, "CLBD"); err != nil {
		return Statement{}, fmt.Errorf("statement %q: %w", st.ID, err)
	}

	for i, e := range raw.Entries {
		if !strings.EqualFold(strings.TrimSpace(e.Status), "BOOK") {
			continue
		}
		tx, err := entryToTransaction(e, account, sourceRef)
		if err != nil {
			return Statement{}, fmt.Errorf("statement %q entry %d: %w", st.ID, i+1, err)
		}
		st.Transactions = append(st.Transactions, tx)
	}

	return st, nil
}

func entryToTransaction(e camtEntry, account, sourceRef string) (Transaction, error) {
	minor, err := parseDecimalMinor(e.Amount.Value)
	if err != nil {
		return Transaction{}, err
	}

	dir := Credit
	if strings.EqualFold(strings.TrimSpace(e.CreditDebit), "DBIT") {
		dir = Debit
	}

	booking, err := parseCamtDate(e.BookingDate)
	if err != nil {
		return Transaction{}, fmt.Errorf("booking date: %w", err)
	}
	value, err := parseCamtDate(e.ValueDate)
	if err != nil {
		return Transaction{}, fmt.Errorf("value date: %w", err)
	}

	tx := Transaction{
		Account:     account,
		BookingDate: booking,
		ValueDate:   value,
		Amount:      domain.Money{MinorUnits: minor, Currency: strings.TrimSpace(e.Amount.Currency)},
		Direction:   dir,
		BankRef:     strings.TrimSpace(e.AcctSvcrRef),
		Source:      SourceCamt053,
		SourceRef:   sourceRef,
	}

	// Entry-level details are optional and banks vary in how many TxDtls they
	// nest. Take the first; a batched entry with several is out of scope and
	// would need splitting rather than flattening.
	if len(e.Details.Transactions) > 0 {
		d := e.Details.Transactions[0]
		tx.EndToEndID = strings.TrimSpace(d.Refs.EndToEndID)
		tx.Remittance = strings.TrimSpace(strings.Join(d.Remittance.Unstructured, " "))

		// The counterparty is whichever side we are not. On a debit the money
		// went to the creditor; on a credit it came from the debtor.
		if dir == Debit {
			tx.Counterparty = strings.TrimSpace(d.Parties.Creditor.Name)
		} else {
			tx.Counterparty = strings.TrimSpace(d.Parties.Debtor.Name)
		}
	}

	return tx, nil
}

// balanceByCode finds the first balance matching any of the given ISO codes.
// Callers pass fallbacks in preference order: OPBD (opening booked) is standard,
// but some banks emit PRCD (previous closing) instead and mean the same thing.
func balanceByCode(balances []camtBalance, codes ...string) (domain.Money, error) {
	for _, want := range codes {
		for _, b := range balances {
			if !strings.EqualFold(strings.TrimSpace(b.Type.CodeOrProprietary.Code), want) {
				continue
			}
			minor, err := parseDecimalMinor(b.Amount.Value)
			if err != nil {
				return domain.Money{}, fmt.Errorf("balance %s: %w", want, err)
			}
			// A balance may itself be negative, expressed as DBIT rather than a
			// minus sign. An overdrawn account read as positive silently
			// inverts every reconciliation on it.
			if strings.EqualFold(strings.TrimSpace(b.CreditDebit), "DBIT") {
				minor = -minor
			}
			return domain.Money{MinorUnits: minor, Currency: strings.TrimSpace(b.Amount.Currency)}, nil
		}
	}
	return domain.Money{}, fmt.Errorf("no balance found with code %s", strings.Join(codes, " or "))
}

// parseCamtDate handles the <Dt><Dt>/<DtTm> choice element: a date-only entry
// or a full timestamp, depending on bank and field.
func parseCamtDate(d camtDate) (time.Time, error) {
	return parseCamtTime(firstNonEmpty(d.Date, d.DateTime))
}

func parseCamtTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	for _, layout := range []string{
		"2006-01-02",
		"2006-01-02T15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05.000",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date format %q", s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// --- wire types -------------------------------------------------------------
//
// Namespace is intentionally omitted from every tag so one parser covers all
// camt.053 minor versions.

type camtDocument struct {
	XMLName    xml.Name        `xml:"Document"`
	Statements []camtStatement `xml:"BkToCstmrStmt>Stmt"`
}

type camtStatement struct {
	ID       string        `xml:"Id"`
	Period   camtPeriod    `xml:"FrToDt"`
	Account  camtAccount   `xml:"Acct"`
	Balances []camtBalance `xml:"Bal"`
	Entries  []camtEntry   `xml:"Ntry"`
}

type camtPeriod struct {
	From string `xml:"FrDtTm"`
	To   string `xml:"ToDtTm"`
}

type camtAccount struct {
	ID struct {
		IBAN string `xml:"IBAN"`
		Othr struct {
			ID string `xml:"Id"`
		} `xml:"Othr"`
	} `xml:"Id"`
	Currency string `xml:"Ccy"`
}

type camtBalance struct {
	Type struct {
		CodeOrProprietary struct {
			Code string `xml:"Cd"`
		} `xml:"CdOrPrtry"`
	} `xml:"Tp"`
	Amount      camtAmount `xml:"Amt"`
	CreditDebit string     `xml:"CdtDbtInd"`
	Date        camtDate   `xml:"Dt"`
}

type camtAmount struct {
	Value    string `xml:",chardata"`
	Currency string `xml:"Ccy,attr"`
}

type camtDate struct {
	Date     string `xml:"Dt"`
	DateTime string `xml:"DtTm"`
}

type camtEntry struct {
	Amount      camtAmount `xml:"Amt"`
	CreditDebit string     `xml:"CdtDbtInd"`
	Status      string     `xml:"Sts"`
	BookingDate camtDate   `xml:"BookgDt"`
	ValueDate   camtDate   `xml:"ValDt"`
	AcctSvcrRef string     `xml:"AcctSvcrRef"`
	Details     struct {
		Transactions []camtTxDetails `xml:"TxDtls"`
	} `xml:"NtryDtls"`
}

type camtTxDetails struct {
	Refs struct {
		EndToEndID string `xml:"EndToEndId"`
	} `xml:"Refs"`
	Parties struct {
		Creditor struct {
			Name string `xml:"Nm"`
		} `xml:"Cdtr"`
		Debtor struct {
			Name string `xml:"Nm"`
		} `xml:"Dbtr"`
	} `xml:"RltdPties"`
	Remittance struct {
		Unstructured []string `xml:"Ustrd"`
	} `xml:"RmtInf"`
}
