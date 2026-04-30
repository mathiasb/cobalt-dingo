// Package fortnox provides a client for the Fortnox REST API v3.
package fortnox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// rateLimiter enforces the Fortnox limit of 25 requests per 5 seconds.
var rateLimiter = struct {
	mu        sync.Mutex
	count     int
	windowEnd time.Time
}{}

// Client is an authenticated Fortnox API client.
//
// readOnly controls a client-side write gate enforced in Client.do: when
// true, any request whose HTTP method is not GET (or HEAD) is rejected
// before it reaches Fortnox. This is defence in depth on top of the
// OAuth scope assigned to the connected app — even if a future tool
// accidentally calls a write method, it cannot mutate live data.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	readOnly   bool
}

// ErrReadOnlyClient is returned by Client.do when a write request is
// attempted on a read-only client. The message is intentionally loud so it
// surfaces clearly in MCP tool results, server logs, and stderr.
var ErrReadOnlyClient = fmt.Errorf("WRITE BLOCKED: this Fortnox client is read-only — set FORTNOX_MODE=sandbox to test writes")

// NewClient returns a Client pointed at baseURL using the given access
// token. Pass readOnly=true to refuse any non-GET request locally.
func NewClient(baseURL, token string, readOnly bool) *Client {
	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{},
		readOnly:   readOnly,
	}
}

// SupplierInvoiceRow is the Fortnox JSON representation of a supplier invoice.
type SupplierInvoiceRow struct {
	InvoiceNumber        int     `json:"InvoiceNumber"`
	SupplierNumber       int     `json:"SupplierNumber"`
	SupplierName         string  `json:"SupplierName"`
	Currency             string  `json:"Currency"`
	TotalInvoiceCurrency float64 `json:"TotalInvoiceCurrency"`
	DueDate              string  `json:"DueDate"`
}

// SupplierInvoicesResponse is the top-level envelope returned by GET /3/supplierinvoices.
type SupplierInvoicesResponse struct {
	SupplierInvoices []SupplierInvoiceRow `json:"SupplierInvoices"`
}

// UnpaidSupplierInvoices fetches all unpaid supplier invoices from Fortnox.
func (c *Client) UnpaidSupplierInvoices() ([]domain.SupplierInvoice, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/3/supplierinvoices?filter=unpaid", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("GET supplierinvoices: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET supplierinvoices: unexpected status %d", resp.StatusCode)
	}

	var envelope SupplierInvoicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode supplierinvoices: %w", err)
	}

	invoices := make([]domain.SupplierInvoice, len(envelope.SupplierInvoices))
	for i, row := range envelope.SupplierInvoices {
		invoices[i] = domain.SupplierInvoice{
			InvoiceNumber:  row.InvoiceNumber,
			SupplierNumber: row.SupplierNumber,
			SupplierName:   row.SupplierName,
			Amount:         domain.MoneyFromFloat(row.TotalInvoiceCurrency, row.Currency),
			DueDate:        row.DueDate,
		}
	}
	return invoices, nil
}

// MetaInformation is the pagination envelope Fortnox includes in list responses.
type MetaInformation struct {
	TotalPages     int `json:"@TotalPages"`
	CurrentPage    int `json:"@CurrentPage"`
	TotalResources int `json:"@TotalResources"`
}

// metaEnvelope is used to extract MetaInformation from a raw response.
type metaEnvelope struct {
	Meta MetaInformation `json:"MetaInformation"`
}

// waitForRate blocks until a request slot is available within the Fortnox
// rate limit. Fortnox documents 25 req / 5 s but appears to enforce it via a
// sliding window — bursting 25 req in <1 s reliably triggers 429s. The cap
// is dialled back to 18 here to leave headroom for that sliding behaviour.
func waitForRate() {
	const maxReqs = 18
	const window = 5 * time.Second

	for {
		rateLimiter.mu.Lock()
		now := time.Now()
		if now.After(rateLimiter.windowEnd) {
			rateLimiter.count = 0
			rateLimiter.windowEnd = now.Add(window)
		}
		if rateLimiter.count < maxReqs {
			rateLimiter.count++
			rateLimiter.mu.Unlock()
			return
		}
		wait := time.Until(rateLimiter.windowEnd)
		rateLimiter.mu.Unlock()
		time.Sleep(wait)
	}
}

// do performs an HTTP request after waiting for a free rate-limit slot.
// All Client methods that hit Fortnox should use this in place of
// c.httpClient.Do(req) so the 25 req / 5 s ceiling is enforced.
//
// Write gate: when c.readOnly is true, any request whose method is not GET
// or HEAD is rejected with ErrReadOnlyClient before any HTTP traffic. This
// is the single chokepoint for the readonly mode safety guarantee — every
// write method on Client routes through here.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.readOnly && req.Method != http.MethodGet && req.Method != http.MethodHead {
		return nil, fmt.Errorf("%w: attempted %s %s", ErrReadOnlyClient, req.Method, req.URL.Path)
	}
	waitForRate()
	return c.httpClient.Do(req)
}

// Get performs an authenticated, rate-limited GET request and returns the raw
// JSON response body.
func (c *Client) Get(requestURL string) (json.RawMessage, error) {
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", requestURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", requestURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return json.RawMessage(body), nil
}

// SupplierInvoicePaymentRow is the Fortnox JSON for a single supplier invoice payment.
type SupplierInvoicePaymentRow struct {
	Number         int     `json:"Number"`
	InvoiceNumber  int     `json:"InvoiceNumber"`
	Amount         float64 `json:"Amount"`
	AmountCurrency float64 `json:"AmountCurrency"`
	Currency       string  `json:"Currency"`
	CurrencyRate   float64 `json:"CurrencyRate"`
	PaymentDate    string  `json:"PaymentDate"`
	Booked         bool    `json:"Booked"`
}

// supplierInvoicePaymentsResponse is the envelope for GET /3/supplierinvoicepayments.
type supplierInvoicePaymentsResponse struct {
	SupplierInvoicePayments []SupplierInvoicePaymentRow `json:"SupplierInvoicePayments"`
}

// ListSupplierInvoicePayments returns all payments recorded against the given invoice number.
// Calls GET /3/supplierinvoicepayments?invoicenumber={n}.
func (c *Client) ListSupplierInvoicePayments(invoiceNumber int) ([]SupplierInvoicePaymentRow, error) {
	u := fmt.Sprintf("%s/3/supplierinvoicepayments?invoicenumber=%d", c.baseURL, invoiceNumber)
	raw, err := c.Get(u)
	if err != nil {
		return nil, fmt.Errorf("list supplier invoice payments: %w", err)
	}
	var envelope supplierInvoicePaymentsResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode supplier invoice payments: %w", err)
	}
	return envelope.SupplierInvoicePayments, nil
}

// FullSupplierRow contains all fields relevant for supplier analysis.
type FullSupplierRow struct {
	SupplierNumber int    `json:"SupplierNumber"`
	Name           string `json:"Name"`
	Email          string `json:"Email"`
	Phone          string `json:"Phone1"`
	IBAN           string `json:"IBAN"`
	BIC            string `json:"BIC"`
	Active         bool   `json:"Active"`
}

// fullSupplierResponse is the envelope for GET /3/suppliers/{n}.
type fullSupplierResponse struct {
	Supplier FullSupplierRow `json:"Supplier"`
}

// GetFullSupplier fetches complete supplier master data for the given supplier number.
// Calls GET /3/suppliers/{n}.
func (c *Client) GetFullSupplier(supplierNumber int) (FullSupplierRow, error) {
	u := fmt.Sprintf("%s/3/suppliers/%d", c.baseURL, supplierNumber)
	raw, err := c.Get(u)
	if err != nil {
		return FullSupplierRow{}, fmt.Errorf("get full supplier: %w", err)
	}
	var envelope fullSupplierResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return FullSupplierRow{}, fmt.Errorf("decode supplier: %w", err)
	}
	return envelope.Supplier, nil
}

// CustomerInvoiceRow is the Fortnox JSON for a customer invoice from GET /3/invoices.
type CustomerInvoiceRow struct {
	DocumentNumber int     `json:"DocumentNumber"`
	CustomerNumber string  `json:"CustomerNumber"` // Fortnox returns this as a string
	CustomerName   string  `json:"CustomerName"`
	Currency       string  `json:"Currency"`
	Total          float64 `json:"Total"`
	Balance        float64 `json:"Balance"`
	DueDate        string  `json:"DueDate"`
	InvoiceDate    string  `json:"InvoiceDate"`
	Booked         bool    `json:"Booked"`
	Cancelled      bool    `json:"Cancelled"`
	Sent           bool    `json:"Sent"`
}

// customerInvoicesResponse is the envelope for GET /3/invoices.
type customerInvoicesResponse struct {
	Invoices []CustomerInvoiceRow `json:"Invoices"`
}

// UnpaidCustomerInvoices fetches all unpaid customer invoices.
// Calls GET /3/invoices?filter=unpaid.
func (c *Client) UnpaidCustomerInvoices() ([]CustomerInvoiceRow, error) {
	u := c.baseURL + "/3/invoices?filter=unpaid"
	raw, err := c.Get(u)
	if err != nil {
		return nil, fmt.Errorf("unpaid customer invoices: %w", err)
	}
	var envelope customerInvoicesResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode customer invoices: %w", err)
	}
	return envelope.Invoices, nil
}

// CustomerInvoicePaymentRow is the Fortnox JSON for a customer invoice payment.
type CustomerInvoicePaymentRow struct {
	Number         int     `json:"Number"`
	InvoiceNumber  int     `json:"InvoiceNumber"`
	Amount         float64 `json:"Amount"`
	AmountCurrency float64 `json:"AmountCurrency"`
	Currency       string  `json:"Currency"`
	PaymentDate    string  `json:"PaymentDate"`
	Booked         bool    `json:"Booked"`
}

// customerInvoicePaymentsResponse is the envelope for GET /3/invoicepayments.
type customerInvoicePaymentsResponse struct {
	InvoicePayments []CustomerInvoicePaymentRow `json:"InvoicePayments"`
}

// ListCustomerInvoicePayments returns all payments received against the given invoice number.
// Calls GET /3/invoicepayments?invoicenumber={n}.
func (c *Client) ListCustomerInvoicePayments(invoiceNumber int) ([]CustomerInvoicePaymentRow, error) {
	u := fmt.Sprintf("%s/3/invoicepayments?invoicenumber=%d", c.baseURL, invoiceNumber)
	raw, err := c.Get(u)
	if err != nil {
		return nil, fmt.Errorf("list customer invoice payments: %w", err)
	}
	var envelope customerInvoicePaymentsResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode customer invoice payments: %w", err)
	}
	return envelope.InvoicePayments, nil
}

// FullCustomerRow contains all fields relevant for customer analysis.
type FullCustomerRow struct {
	CustomerNumber string `json:"CustomerNumber"` // Fortnox returns this as a string
	Name           string `json:"Name"`
	Email          string `json:"Email"`
	Phone          string `json:"Phone1"`
	Active         bool   `json:"Active"`
}

// fullCustomerResponse is the envelope for GET /3/customers/{n}.
type fullCustomerResponse struct {
	Customer FullCustomerRow `json:"Customer"`
}

// GetFullCustomer fetches complete customer master data for the given customer number.
// Calls GET /3/customers/{n}.
func (c *Client) GetFullCustomer(customerNumber int) (FullCustomerRow, error) {
	u := fmt.Sprintf("%s/3/customers/%d", c.baseURL, customerNumber)
	raw, err := c.Get(u)
	if err != nil {
		return FullCustomerRow{}, fmt.Errorf("get full customer: %w", err)
	}
	var envelope fullCustomerResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return FullCustomerRow{}, fmt.Errorf("decode customer: %w", err)
	}
	return envelope.Customer, nil
}

// AccountRow is the Fortnox JSON for a GL account from GET /3/accounts.
type AccountRow struct {
	Number                int     `json:"Number"`
	Description           string  `json:"Description"`
	SRU                   int     `json:"SRU"`
	Active                bool    `json:"Active"`
	BalanceBroughtForward float64 `json:"BalanceBroughtForward"`
	BalanceCarriedForward float64 `json:"BalanceCarriedForward"`
}

// accountsResponse is the envelope for GET /3/accounts.
type accountsResponse struct {
	Accounts []AccountRow `json:"Accounts"`
}

// ListAccounts fetches the chart of accounts for a financial year.
// Calls GET /3/accounts?financialyear={yearID}.
func (c *Client) ListAccounts(yearID int) ([]AccountRow, error) {
	u := fmt.Sprintf("%s/3/accounts?financialyear=%d", c.baseURL, yearID)
	raw, err := c.Get(u)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	var envelope accountsResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode accounts: %w", err)
	}
	return envelope.Accounts, nil
}

// FinancialYearRow is the Fortnox JSON for a financial year from GET /3/financialyears.
type FinancialYearRow struct {
	ID       int    `json:"Id"`
	FromDate string `json:"FromDate"`
	ToDate   string `json:"ToDate"`
}

// financialYearsResponse is the envelope for GET /3/financialyears.
type financialYearsResponse struct {
	FinancialYears []FinancialYearRow `json:"FinancialYears"`
}

// ListFinancialYears returns all financial years for the company.
// Calls GET /3/financialyears.
func (c *Client) ListFinancialYears() ([]FinancialYearRow, error) {
	u := c.baseURL + "/3/financialyears"
	raw, err := c.Get(u)
	if err != nil {
		return nil, fmt.Errorf("list financial years: %w", err)
	}
	var envelope financialYearsResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode financial years: %w", err)
	}
	return envelope.FinancialYears, nil
}

// GetAllPages fetches every page from a paginated Fortnox endpoint. It sets
// the `page` query parameter on successive requests and reads MetaInformation
// from each response to determine when to stop. One json.RawMessage per page
// is returned.
func (c *Client) GetAllPages(baseURL string) ([]json.RawMessage, error) {
	var pages []json.RawMessage

	for page := 1; ; page++ {
		u, err := url.Parse(baseURL)
		if err != nil {
			return nil, fmt.Errorf("parse URL: %w", err)
		}
		q := u.Query()
		q.Set("page", strconv.Itoa(page))
		u.RawQuery = q.Encode()

		raw, err := c.Get(u.String())
		if err != nil {
			return nil, fmt.Errorf("get page %d: %w", page, err)
		}
		pages = append(pages, raw)

		var env metaEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("decode meta page %d: %w", page, err)
		}

		if page >= env.Meta.TotalPages || env.Meta.TotalPages == 0 {
			break
		}
	}

	return pages, nil
}
