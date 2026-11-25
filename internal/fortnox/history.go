package fortnox

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// The reads in this file exist for the history probe (cmd/history-eval, ADR-0013
// spike 1). All are GET, so the write gate in Client.do never has anything to
// refuse, and every list goes through listAll so no page is skipped (#90).

// SupplierInvoiceHead is the header of a supplier invoice as the list endpoint
// returns it, including the link to the voucher it was booked as.
type SupplierInvoiceHead struct {
	GivenNumber    string
	SupplierNumber int
	SupplierName   string
	InvoiceDate    string
	Currency       string
	Booked         bool
	Cancelled      bool
	VoucherSeries  string
	VoucherNumber  int
	VoucherYear    int
}

type supplierInvoiceHeadJSON struct {
	GivenNumber    FlexString `json:"GivenNumber"`
	SupplierNumber FlexInt    `json:"SupplierNumber"`
	SupplierName   FlexString `json:"SupplierName"`
	InvoiceDate    FlexString `json:"InvoiceDate"`
	Currency       FlexString `json:"Currency"`
	Booked         bool       `json:"Booked"`
	Cancelled      bool       `json:"Cancelled"`
	VoucherSeries  FlexString `json:"VoucherSeries"`
	VoucherNumber  FlexInt    `json:"VoucherNumber"`
	VoucherYear    FlexInt    `json:"VoucherYear"`
}

// ListSupplierInvoiceHeads returns every supplier invoice dated within
// [fromDate, toDate] (YYYY-MM-DD). Calls GET /3/supplierinvoices.
func (c *Client) ListSupplierInvoiceHeads(fromDate, toDate string) ([]SupplierInvoiceHead, error) {
	q := url.Values{}
	q.Set("fromdate", fromDate)
	q.Set("todate", toDate)
	return listAll(c, c.baseURL+"/3/supplierinvoices?"+q.Encode(), "supplier invoices",
		func(raw json.RawMessage) ([]SupplierInvoiceHead, error) {
			return decodeInto(raw, func(e struct {
				SupplierInvoices []supplierInvoiceHeadJSON `json:"SupplierInvoices"`
			},
			) []SupplierInvoiceHead {
				out := make([]SupplierInvoiceHead, 0, len(e.SupplierInvoices))
				for _, h := range e.SupplierInvoices {
					out = append(out, SupplierInvoiceHead{
						GivenNumber: string(h.GivenNumber), SupplierNumber: int(h.SupplierNumber),
						SupplierName: string(h.SupplierName), InvoiceDate: string(h.InvoiceDate),
						Currency: string(h.Currency), Booked: h.Booked, Cancelled: h.Cancelled,
						VoucherSeries: string(h.VoucherSeries), VoucherNumber: int(h.VoucherNumber), VoucherYear: int(h.VoucherYear),
					})
				}
				return out
			})
		})
}

// SupplierInvoiceRowDetail is one accounting row of a supplier invoice, in the
// company's base currency.
type SupplierInvoiceRowDetail struct {
	Account    int
	Debit      float64
	Credit     float64
	CostCenter string
	Project    string
}

// GetSupplierInvoiceRows returns the accounting rows of one supplier invoice.
// Calls GET /3/supplierinvoices/{givenNumber}.
func (c *Client) GetSupplierInvoiceRows(givenNumber string) ([]SupplierInvoiceRowDetail, error) {
	raw, err := c.get(c.baseURL + "/3/supplierinvoices/" + url.PathEscape(givenNumber))
	if err != nil {
		return nil, fmt.Errorf("get supplier invoice %s: %w", givenNumber, err)
	}
	var env struct {
		SupplierInvoice struct {
			Rows []struct {
				Account    FlexInt    `json:"Account"`
				Debit      FlexFloat  `json:"Debit"`
				Credit     FlexFloat  `json:"Credit"`
				CostCenter FlexString `json:"CostCenter"`
				Project    FlexString `json:"Project"`
			} `json:"SupplierInvoiceRows"`
		} `json:"SupplierInvoice"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode supplier invoice %s: %w", givenNumber, err)
	}
	out := make([]SupplierInvoiceRowDetail, 0, len(env.SupplierInvoice.Rows))
	for _, r := range env.SupplierInvoice.Rows {
		out = append(out, SupplierInvoiceRowDetail{
			Account: int(r.Account), Debit: float64(r.Debit), Credit: float64(r.Credit),
			CostCenter: string(r.CostCenter), Project: string(r.Project),
		})
	}
	return out, nil
}

// SupplierIdentity says which identifying fields a supplier record carries. It
// holds presence flags and the predefined account, never a name, organisation
// number, VAT number or bank detail, so a report built from it cannot leak one.
type SupplierIdentity struct {
	SupplierNumber        int
	HasOrganisationNumber bool
	HasVATNumber          bool
	HasBankAccount        bool
	HasIBAN               bool
	PreDefinedAccount     int // 0 when none
	CountryCode           string
}

// ListSupplierIdentities returns the identity flags of every supplier.
// Calls GET /3/suppliers.
func (c *Client) ListSupplierIdentities() ([]SupplierIdentity, error) {
	return listAll(c, c.baseURL+"/3/suppliers", "suppliers",
		func(raw json.RawMessage) ([]SupplierIdentity, error) {
			return decodeInto(raw, func(e struct {
				Suppliers []struct {
					SupplierNumber     FlexInt    `json:"SupplierNumber"`
					OrganisationNumber FlexString `json:"OrganisationNumber"`
					VATNumber          FlexString `json:"VATNumber"`
					BankAccountNumber  FlexString `json:"BankAccountNumber"`
					IBAN               FlexString `json:"IBAN"`
					PreDefinedAccount  FlexInt    `json:"PreDefinedAccount"`
					CountryCode        FlexString `json:"CountryCode"`
				} `json:"Suppliers"`
			},
			) []SupplierIdentity {
				out := make([]SupplierIdentity, 0, len(e.Suppliers))
				for _, s := range e.Suppliers {
					out = append(out, SupplierIdentity{
						SupplierNumber:        int(s.SupplierNumber),
						HasOrganisationNumber: s.OrganisationNumber != "",
						HasVATNumber:          s.VATNumber != "",
						HasBankAccount:        s.BankAccountNumber != "",
						HasIBAN:               s.IBAN != "",
						PreDefinedAccount:     int(s.PreDefinedAccount),
						CountryCode:           string(s.CountryCode),
					})
				}
				return out
			})
		})
}
