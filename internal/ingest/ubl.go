package ingest

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
)

const (
	// MaxXMLBytes bounds an e-invoice. A real Peppol invoice is a few kilobytes
	// to a few hundred; a megabyte leaves room for attachments referenced inline
	// that are never read here.
	MaxXMLBytes = 1 << 20

	// maxXMLDepth bounds element nesting. UBL invoices nest about six deep.
	maxXMLDepth = 32

	// maxTextLen matches extract's bound on the supplier and number: they flow
	// into a page, a journal text and a CSV.
	maxTextLen = 200

	// maxElementText bounds the text kept for any one element read.
	maxElementText = 1024
)

// ParseUBL reads a Peppol BIS Billing 3.0 / UBL 2.1 Invoice or CreditNote
// deterministically, with no model. It does not repair anything: a missing,
// malformed or out-of-range field is an error, and an invoice whose total does
// not add up is returned as read so the usual reconciliation refuses it.
//
// Amounts in UBL are unsigned and the document type says whether it is a credit
// note, so a credit note's net, VAT and total come back negative, which is what
// the entry builder refuses as a credit note.
//
// Hardening: a size cap, a depth cap, no DOCTYPE at all (so no external or
// internal entity can be declared), and Go's decoder resolves only the five
// predefined entities and never fetches anything.
func ParseUBL(data []byte) (extract.Invoice, error) {
	if len(data) > MaxXMLBytes {
		return extract.Invoice{}, fmt.Errorf("e-invoice is larger than %d bytes", MaxXMLBytes)
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true

	var (
		path     []string
		text     strings.Builder
		capture  bool
		credit   bool
		sawRoot  bool
		fields   = map[string]string{}
		taxTotal = map[string]string{} // currencyID -> first TaxTotal/TaxAmount
		taxCur   string
	)
	want := map[string]string{
		"ID":                   "number",
		"InvoiceTypeCode":      "typeCode",
		"CreditNoteTypeCode":   "typeCode",
		"IssueDate":            "IssueDate",
		"DueDate":              "DueDate",
		"DocumentCurrencyCode": "currency",
		"AccountingSupplierParty/Party/PartyLegalEntity/RegistrationName": "supplier",
		"AccountingSupplierParty/Party/PartyName/Name":                    "supplierAlt",
		"LegalMonetaryTotal/TaxExclusiveAmount":                           "net",
		"LegalMonetaryTotal/LineExtensionAmount":                          "netAlt",
		"LegalMonetaryTotal/PayableAmount":                                "PayableAmount",
		"TaxTotal/TaxAmount":                                              "tax",
	}
	var key string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return extract.Invoice{}, fmt.Errorf("not valid XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			if bytes.HasPrefix(bytes.ToUpper(bytes.TrimSpace(t)), []byte("DOCTYPE")) {
				return extract.Invoice{}, errors.New("a DOCTYPE is not allowed in an e-invoice")
			}
		case xml.StartElement:
			if len(path) >= maxXMLDepth {
				return extract.Invoice{}, fmt.Errorf("e-invoice nests deeper than %d elements", maxXMLDepth)
			}
			if !sawRoot {
				sawRoot = true
				if ublRoots[t.Name.Space] != t.Name.Local {
					return extract.Invoice{}, errors.New("not a UBL Invoice or CreditNote")
				}
				credit = t.Name.Local == "CreditNote"
			}
			path = append(path, t.Name.Local)
			rel := strings.Join(path[1:], "/")
			key, capture = want[rel]
			if capture && key == "tax" {
				taxCur = ""
				for _, a := range t.Attr {
					if a.Name.Local == "currencyID" {
						taxCur = a.Value
					}
				}
			}
			text.Reset()
		case xml.CharData:
			if capture {
				if text.Len()+len(t) > maxElementText {
					return extract.Invoice{}, fmt.Errorf("the text of one element is longer than %d bytes", maxElementText)
				}
				text.Write(t)
			}
		case xml.EndElement:
			if capture {
				v := strings.TrimSpace(text.String())
				if key == "tax" {
					if _, seen := taxTotal[taxCur]; !seen {
						taxTotal[taxCur] = v
					}
				} else if _, seen := fields[key]; !seen {
					fields[key] = v
				}
			}
			capture = false
			text.Reset()
			path = path[:len(path)-1]
		}
	}
	if !sawRoot {
		return extract.Invoice{}, errors.New("not valid XML: no root element")
	}

	var inv extract.Invoice
	var err error
	if inv.InvoiceNumber, err = boundedText("ID", fields["number"]); err != nil {
		return extract.Invoice{}, err
	}
	supplier := fields["supplier"]
	if supplier == "" {
		supplier = fields["supplierAlt"]
	}
	if inv.Supplier, err = boundedText("supplier", supplier); err != nil {
		return extract.Invoice{}, err
	}
	if inv.InvoiceDate, err = isoDate("IssueDate", fields["IssueDate"], true); err != nil {
		return extract.Invoice{}, err
	}
	if inv.DueDate, err = isoDate("DueDate", fields["DueDate"], false); err != nil {
		return extract.Invoice{}, err
	}
	if !isCurrency(fields["currency"]) {
		return extract.Invoice{}, fmt.Errorf("DocumentCurrencyCode %q is not an ISO currency code", fields["currency"])
	}
	inv.Currency = fields["currency"]

	// The document element and its type code must agree about whether this is a
	// credit note: the sign of every amount rests on it.
	code := fields["typeCode"]
	switch {
	case code == "":
		return extract.Invoice{}, errors.New("the document type code is missing")
	case credit != creditTypeCodes[code]:
		return extract.Invoice{}, fmt.Errorf("the document type code %q does not match a %s", code, map[bool]string{true: "CreditNote", false: "Invoice"}[credit])
	}
	sign := int64(1)
	if credit {
		sign = -1
	}
	if inv.Total, err = unsigned("PayableAmount", fields["PayableAmount"], sign); err != nil {
		return extract.Invoice{}, err
	}
	vat, ok := taxTotal[inv.Currency]
	if !ok {
		return extract.Invoice{}, fmt.Errorf("TaxTotal: none in the document currency %s", inv.Currency)
	}
	if inv.VATAmount, err = unsigned("TaxTotal", vat, sign); err != nil {
		return extract.Invoice{}, err
	}
	// The net amount is optional only in the sense that Reconciled then says it
	// cannot be checked; UBL has two places for it.
	net := fields["net"]
	if net == "" {
		net = fields["netAlt"]
	}
	if net != "" {
		if inv.NetTotal, err = unsigned("net amount", net, sign); err != nil {
			return extract.Invoice{}, err
		}
		inv.NetKnown = true
	}
	return inv, nil
}

// creditTypeCodes are the UN/CEFACT 1001 codes that mean a credit.
var creditTypeCodes = map[string]bool{
	"381": true, "396": true, "261": true, "262": true, "296": true, "308": true, "420": true, "458": true, "532": true,
}

// plainDecimal is the only amount shape UBL allows here: digits, then at most
// two decimals after a point. No spaces, commas or exponents.
var plainDecimal = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)

func boundedText(name, v string) (string, error) {
	if v == "" {
		return "", fmt.Errorf("%s is missing", name)
	}
	if len(v) > maxTextLen {
		return "", fmt.Errorf("%s is longer than %d bytes", name, maxTextLen)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%s contains a control character", name)
		}
	}
	return v, nil
}

func isoDate(name, v string, required bool) (string, error) {
	if v == "" {
		if required {
			return "", fmt.Errorf("%s is missing", name)
		}
		return "", nil
	}
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return "", fmt.Errorf("%s %q is not a YYYY-MM-DD date", name, v)
	}
	return d.Format("2006-01-02"), nil
}

// unsigned reads a UBL amount, which carries no sign, and applies the sign the
// document type gives it. A sign in the text is refused, not honoured.
func unsigned(name, v string, sign int64) (int64, error) {
	if v == "" {
		return 0, fmt.Errorf("%s is missing", name)
	}
	if strings.HasPrefix(v, "-") || strings.HasPrefix(v, "+") {
		return 0, fmt.Errorf("%s %q carries a sign; UBL amounts are unsigned", name, v)
	}
	if !plainDecimal.MatchString(v) {
		return 0, fmt.Errorf("%s %q is not a plain decimal with at most two places", name, v)
	}
	n, err := extract.ParseAmount(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n * sign, nil
}

func isCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
