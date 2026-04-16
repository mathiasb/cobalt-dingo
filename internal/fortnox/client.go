// Package fortnox provides a client for the Fortnox REST API v3.
package fortnox

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mathiasb/cobalt-dingo/internal/invoice"
)

// Client is an authenticated Fortnox API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient returns a Client pointed at baseURL using the given access token.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{},
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
func (c *Client) UnpaidSupplierInvoices() ([]invoice.SupplierInvoice, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/3/supplierinvoices?filter=unpaid", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
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

	invoices := make([]invoice.SupplierInvoice, len(envelope.SupplierInvoices))
	for i, row := range envelope.SupplierInvoices {
		invoices[i] = invoice.SupplierInvoice{
			InvoiceNumber:  row.InvoiceNumber,
			SupplierNumber: row.SupplierNumber,
			SupplierName:   row.SupplierName,
			Currency:       row.Currency,
			Total:          row.TotalInvoiceCurrency,
			DueDate:        row.DueDate,
		}
	}
	return invoices, nil
}
