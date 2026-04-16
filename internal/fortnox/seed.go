package fortnox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SupplierCreate holds the fields needed to create a new supplier.
type SupplierCreate struct {
	Name        string
	CountryCode string
	IBAN        string
	BIC         string
}

// CreateSupplier creates a new supplier and returns its assigned SupplierNumber.
func (c *Client) CreateSupplier(s SupplierCreate) (int, error) {
	body := map[string]any{
		"Supplier": map[string]any{
			"Name":        s.Name,
			"CountryCode": s.CountryCode,
			"IBAN":        s.IBAN,
			"BIC":         s.BIC,
		},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/suppliers", bytes.NewReader(b))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("POST supplier: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Supplier struct {
			SupplierNumber string `json:"SupplierNumber"`
		} `json:"Supplier"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return 0, fmt.Errorf("decode supplier response: %w", err)
	}
	if envelope.Supplier.SupplierNumber == "" {
		return 0, fmt.Errorf("POST supplier: status %d", resp.StatusCode)
	}
	var num int
	fmt.Sscanf(envelope.Supplier.SupplierNumber, "%d", &num)
	return num, nil
}

// DeactivateSupplier sets Active=false on a supplier, hiding it and its invoices
// from operational views. Used by teardown instead of delete, since Fortnox
// does not allow deleting suppliers that have any associated invoices.
func (c *Client) DeactivateSupplier(supplierNumber int) error {
	url := fmt.Sprintf("%s/3/suppliers/%d", c.baseURL, supplierNumber)
	b, _ := json.Marshal(map[string]any{"Supplier": map[string]any{"Active": false}})
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deactivate supplier %d: %w", supplierNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deactivate supplier %d: status %d", supplierNumber, resp.StatusCode)
	}
	return nil
}

// ActivateSupplier sets Active=true on a supplier, reversing DeactivateSupplier.
func (c *Client) ActivateSupplier(supplierNumber int) error {
	url := fmt.Sprintf("%s/3/suppliers/%d", c.baseURL, supplierNumber)
	b, _ := json.Marshal(map[string]any{"Supplier": map[string]any{"Active": true}})
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("activate supplier %d: %w", supplierNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("activate supplier %d: status %d", supplierNumber, resp.StatusCode)
	}
	return nil
}

// SupplierInvoiceCreate holds the fields needed to create a supplier invoice.
type SupplierInvoiceCreate struct {
	SupplierNumber int
	InvoiceDate    string // YYYY-MM-DD
	DueDate        string // YYYY-MM-DD
	Currency       string
	Description    string
	Total          float64
}

// CreateSupplierInvoice creates a supplier invoice and returns its GivenNumber.
func (c *Client) CreateSupplierInvoice(inv SupplierInvoiceCreate) (string, error) {
	body := map[string]any{
		"SupplierInvoice": map[string]any{
			"SupplierNumber": fmt.Sprintf("%d", inv.SupplierNumber),
			"InvoiceDate":    inv.InvoiceDate,
			"DueDate":        inv.DueDate,
			"Currency":       inv.Currency,
			"Comments":       inv.Description,
			"Total":          inv.Total, // drives the AP credit row (account 2440)
			"SupplierInvoiceRows": []map[string]any{
				{
					"Account": 4000, // purchases — provides the expense debit side
					"Total":   inv.Total,
				},
			},
		},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/supplierinvoices", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST supplierinvoice: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		SupplierInvoice struct {
			GivenNumber string `json:"GivenNumber"`
		} `json:"SupplierInvoice"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("decode supplierinvoice response: %w", err)
	}
	if envelope.SupplierInvoice.GivenNumber == "" {
		return "", fmt.Errorf("POST supplierinvoice: status %d", resp.StatusCode)
	}
	return envelope.SupplierInvoice.GivenNumber, nil
}

// SupplierSummary is a minimal supplier record returned by list operations.
type SupplierSummary struct {
	SupplierNumber int
	Name           string
	Active         bool
}

// ListSuppliers returns all suppliers (active and inactive) whose names start with prefix.
func (c *Client) ListSuppliers(prefix string) ([]SupplierSummary, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/3/suppliers", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET suppliers: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Suppliers []struct {
			SupplierNumber string `json:"SupplierNumber"`
			Name           string `json:"Name"`
			Active         bool   `json:"Active"`
		} `json:"Suppliers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode suppliers: %w", err)
	}

	var result []SupplierSummary
	for _, s := range envelope.Suppliers {
		if strings.HasPrefix(s.Name, prefix) {
			var num int
			fmt.Sscanf(s.SupplierNumber, "%d", &num)
			result = append(result, SupplierSummary{SupplierNumber: num, Name: s.Name, Active: s.Active})
		}
	}
	return result, nil
}

// ListSupplierInvoicesBySupplier returns all invoice GivenNumbers for the given supplier number.
func (c *Client) ListSupplierInvoicesBySupplier(supplierNumber int) ([]string, error) {
	url := fmt.Sprintf("%s/3/supplierinvoices?suppliernumber=%d", c.baseURL, supplierNumber)
	req, err := http.NewRequest(http.MethodGet, url, nil)
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

	var envelope struct {
		SupplierInvoices []struct {
			GivenNumber string `json:"GivenNumber"`
		} `json:"SupplierInvoices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode supplierinvoices: %w", err)
	}

	var numbers []string
	for _, inv := range envelope.SupplierInvoices {
		numbers = append(numbers, inv.GivenNumber)
	}
	return numbers, nil
}

// BookkeepSupplierInvoice posts a supplier invoice to the general ledger,
// making it visible in the unpaid filter and payable via the payments API.
func (c *Client) BookkeepSupplierInvoice(givenNumber string) error {
	url := fmt.Sprintf("%s/3/supplierinvoices/%s/bookkeep", c.baseURL, givenNumber)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bookkeep invoice %s: %w", givenNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bookkeep invoice %s: status %d", givenNumber, resp.StatusCode)
	}
	return nil
}

// FullyPaySupplierInvoice creates a full payment for a supplier invoice so that
// the invoice is considered paid and the supplier can be deleted during teardown.
func (c *Client) FullyPaySupplierInvoice(givenNumber string) error {
	// Fetch invoice to get balance and currency rate.
	url := fmt.Sprintf("%s/3/supplierinvoices/%s", c.baseURL, givenNumber)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET invoice %s: %w", givenNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var inv struct {
		SupplierInvoice struct {
			Balance      json.Number `json:"Balance"`
			Total        json.Number `json:"Total"`
			Currency     string      `json:"Currency"`
			CurrencyRate json.Number `json:"CurrencyRate"`
			CurrencyUnit json.Number `json:"CurrencyUnit"`
		} `json:"SupplierInvoice"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
		return fmt.Errorf("decode invoice %s: %w", givenNumber, err)
	}

	balance, _ := inv.SupplierInvoice.Balance.Float64()
	if balance == 0 {
		return nil // already paid or zero-total invoice
	}

	rate, _ := inv.SupplierInvoice.CurrencyRate.Float64()
	unit, _ := inv.SupplierInvoice.CurrencyUnit.Float64()
	balanceCurrency := balance
	if rate > 0 && inv.SupplierInvoice.Currency != "SEK" {
		balanceCurrency = balance / rate * unit
	}

	payment := map[string]any{
		"SupplierInvoicePayment": map[string]any{
			"InvoiceNumber":  givenNumber,
			"Amount":         balance,
			"AmountCurrency": balanceCurrency,
			"PaymentDate":    "2026-04-30",
		},
	}
	b, _ := json.Marshal(payment)
	req2, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/supplierinvoicepayments", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build payment request: %w", err)
	}
	req2.Header.Set("Authorization", "Bearer "+c.token)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "application/json")

	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		return fmt.Errorf("POST payment for invoice %s: %w", givenNumber, err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusCreated && resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("POST payment for invoice %s: status %d", givenNumber, resp2.StatusCode)
	}
	return nil
}
