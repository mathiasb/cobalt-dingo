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

	resp, err := c.do(req)
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
	_, _ = fmt.Sscanf(envelope.Supplier.SupplierNumber, "%d", &num)
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

	resp, err := c.do(req)
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

	resp, err := c.do(req)
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

	resp, err := c.do(req)
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

	resp, err := c.do(req)
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
			_, _ = fmt.Sscanf(s.SupplierNumber, "%d", &num)
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

	resp, err := c.do(req)
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

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("bookkeep invoice %s: %w", givenNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bookkeep invoice %s: status %d", givenNumber, resp.StatusCode)
	}
	return nil
}

// CustomerCreate holds the fields needed to create a new customer.
type CustomerCreate struct {
	Name        string
	CountryCode string
	Currency    string
}

// CreateCustomer creates a new customer and returns its assigned CustomerNumber.
func (c *Client) CreateCustomer(cu CustomerCreate) (int, error) {
	body := map[string]any{
		"Customer": map[string]any{
			"Name":        cu.Name,
			"CountryCode": cu.CountryCode,
			"Currency":    cu.Currency,
		},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/customers", bytes.NewReader(b))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return 0, fmt.Errorf("POST customer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Customer struct {
			CustomerNumber string `json:"CustomerNumber"`
		} `json:"Customer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return 0, fmt.Errorf("decode customer response: %w", err)
	}
	if envelope.Customer.CustomerNumber == "" {
		return 0, fmt.Errorf("POST customer: status %d", resp.StatusCode)
	}
	var num int
	_, _ = fmt.Sscanf(envelope.Customer.CustomerNumber, "%d", &num)
	return num, nil
}

// SetCustomerActive toggles a customer's Active flag. Used by teardown.
func (c *Client) SetCustomerActive(customerNumber int, active bool) error {
	url := fmt.Sprintf("%s/3/customers/%d", c.baseURL, customerNumber)
	b, _ := json.Marshal(map[string]any{"Customer": map[string]any{"Active": active}})
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("set customer %d active=%v: %w", customerNumber, active, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set customer %d active=%v: status %d", customerNumber, active, resp.StatusCode)
	}
	return nil
}

// CustomerSummary is a minimal customer record returned by list operations.
type CustomerSummary struct {
	CustomerNumber int
	Name           string
	Active         bool
}

// ListCustomers returns active customers whose names start with prefix.
// Note: Fortnox's /3/customers does not support listing inactive customers via
// query parameters — for full coverage, list active separately and accept that
// inactive customers are invisible until explicitly reactivated.
func (c *Client) ListCustomers(prefix string) ([]CustomerSummary, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/3/customers", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("GET customers: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Customers []struct {
			CustomerNumber string `json:"CustomerNumber"`
			Name           string `json:"Name"`
			Active         bool   `json:"Active"`
		} `json:"Customers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode customers: %w", err)
	}

	var result []CustomerSummary
	for _, cu := range envelope.Customers {
		if strings.HasPrefix(cu.Name, prefix) {
			var num int
			_, _ = fmt.Sscanf(cu.CustomerNumber, "%d", &num)
			result = append(result, CustomerSummary{CustomerNumber: num, Name: cu.Name, Active: cu.Active})
		}
	}
	return result, nil
}

// CustomerInvoiceCreate holds the fields needed to create a customer invoice.
type CustomerInvoiceCreate struct {
	CustomerNumber int
	InvoiceDate    string // YYYY-MM-DD
	DueDate        string // YYYY-MM-DD
	Currency       string
	Description    string
	Total          float64
}

// CreateCustomerInvoice creates a customer invoice and returns its DocumentNumber.
func (c *Client) CreateCustomerInvoice(inv CustomerInvoiceCreate) (string, error) {
	body := map[string]any{
		"Invoice": map[string]any{
			"CustomerNumber": fmt.Sprintf("%d", inv.CustomerNumber),
			"InvoiceDate":    inv.InvoiceDate,
			"DueDate":        inv.DueDate,
			"Currency":       inv.Currency,
			"Comments":       inv.Description,
			"InvoiceRows": []map[string]any{
				{
					"Description":       inv.Description,
					"AccountNumber":     3001, // sales — service revenue
					"DeliveredQuantity": 1,
					"Price":             inv.Total,
				},
			},
		},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/invoices", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("POST invoice: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Invoice struct {
			DocumentNumber string `json:"DocumentNumber"`
		} `json:"Invoice"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("decode invoice response: %w", err)
	}
	if envelope.Invoice.DocumentNumber == "" {
		return "", fmt.Errorf("POST invoice: status %d", resp.StatusCode)
	}
	return envelope.Invoice.DocumentNumber, nil
}

// BookkeepCustomerInvoice posts a customer invoice to the general ledger.
func (c *Client) BookkeepCustomerInvoice(documentNumber string) error {
	url := fmt.Sprintf("%s/3/invoices/%s/bookkeep", c.baseURL, documentNumber)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("bookkeep invoice %s: %w", documentNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bookkeep invoice %s: status %d", documentNumber, resp.StatusCode)
	}
	return nil
}

// CancelCustomerInvoice cancels a customer invoice (Fortnox does not allow delete).
func (c *Client) CancelCustomerInvoice(documentNumber string) error {
	url := fmt.Sprintf("%s/3/invoices/%s/cancel", c.baseURL, documentNumber)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("cancel invoice %s: %w", documentNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cancel invoice %s: status %d", documentNumber, resp.StatusCode)
	}
	return nil
}

// FullyPayCustomerInvoice creates a full payment for a customer invoice.
func (c *Client) FullyPayCustomerInvoice(documentNumber string, paymentDate string) error {
	url := fmt.Sprintf("%s/3/invoices/%s", c.baseURL, documentNumber)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("GET invoice %s: %w", documentNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var inv struct {
		Invoice struct {
			Balance      json.Number `json:"Balance"`
			Total        json.Number `json:"Total"`
			Currency     string      `json:"Currency"`
			CurrencyRate json.Number `json:"CurrencyRate"`
			CurrencyUnit json.Number `json:"CurrencyUnit"`
		} `json:"Invoice"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
		return fmt.Errorf("decode invoice %s: %w", documentNumber, err)
	}

	// Customer-invoice Balance is in invoice currency (unlike supplier invoices
	// where Balance is in SEK base). Convert to SEK for the Amount field.
	balanceInCurrency, _ := inv.Invoice.Balance.Float64()
	if balanceInCurrency == 0 {
		return nil // already paid
	}

	rate, _ := inv.Invoice.CurrencyRate.Float64()
	unit, _ := inv.Invoice.CurrencyUnit.Float64()
	amountSEK := balanceInCurrency
	if rate > 0 && unit > 0 && inv.Invoice.Currency != "SEK" {
		amountSEK = balanceInCurrency * rate / unit
	}

	payment := map[string]any{
		"InvoicePayment": map[string]any{
			"InvoiceNumber":  documentNumber,
			"Amount":         amountSEK,
			"AmountCurrency": balanceInCurrency,
			"PaymentDate":    paymentDate,
			"ModeOfPayment":  "BG", // Bankgiro — present in standard sandbox setup
		},
	}
	b, _ := json.Marshal(payment)
	req2, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/invoicepayments", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build payment request: %w", err)
	}
	req2.Header.Set("Authorization", "Bearer "+c.token)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "application/json")

	resp2, err := c.do(req2)
	if err != nil {
		return fmt.Errorf("POST customer invoice payment %s: %w", documentNumber, err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusCreated && resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("POST customer invoice payment %s: status %d", documentNumber, resp2.StatusCode)
	}

	var payResp struct {
		InvoicePayment struct {
			Number json.Number `json:"Number"`
		} `json:"InvoicePayment"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&payResp); err != nil {
		return fmt.Errorf("decode payment response %s: %w", documentNumber, err)
	}
	paymentNumber := payResp.InvoicePayment.Number.String()
	if paymentNumber == "" {
		return fmt.Errorf("payment registered for invoice %s but no Number in response", documentNumber)
	}

	// Bookkeep the payment so it reduces the invoice balance.
	bookkeepURL := fmt.Sprintf("%s/3/invoicepayments/%s/bookkeep", c.baseURL, paymentNumber)
	req3, err := http.NewRequest(http.MethodPut, bookkeepURL, nil)
	if err != nil {
		return fmt.Errorf("build bookkeep request: %w", err)
	}
	req3.Header.Set("Authorization", "Bearer "+c.token)
	req3.Header.Set("Accept", "application/json")
	resp3, err := c.do(req3)
	if err != nil {
		return fmt.Errorf("bookkeep payment %s: %w", paymentNumber, err)
	}
	defer func() { _ = resp3.Body.Close() }()
	if resp3.StatusCode != http.StatusOK {
		return fmt.Errorf("bookkeep payment %s: status %d", paymentNumber, resp3.StatusCode)
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

	resp, err := c.do(req)
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

	resp2, err := c.do(req2)
	if err != nil {
		return fmt.Errorf("POST payment for invoice %s: %w", givenNumber, err)
	}
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusCreated && resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("POST payment for invoice %s: status %d", givenNumber, resp2.StatusCode)
	}
	return nil
}

// ProjectCreate holds the fields needed to create a project.
type ProjectCreate struct {
	Description string
	StartDate   string // YYYY-MM-DD, optional
	EndDate     string // YYYY-MM-DD, optional
	Status      string // NOTSTARTED, ONGOING, COMPLETED
}

// CreateProject creates a project and returns its assigned ProjectNumber.
func (c *Client) CreateProject(p ProjectCreate) (string, error) {
	fields := map[string]any{"Description": p.Description}
	if p.StartDate != "" {
		fields["StartDate"] = p.StartDate
	}
	if p.EndDate != "" {
		fields["EndDate"] = p.EndDate
	}
	if p.Status != "" {
		fields["Status"] = p.Status
	}
	b, _ := json.Marshal(map[string]any{"Project": fields})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/projects", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("POST project: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Project struct {
			ProjectNumber string `json:"ProjectNumber"`
		} `json:"Project"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("decode project response: %w", err)
	}
	if envelope.Project.ProjectNumber == "" {
		return "", fmt.Errorf("POST project: status %d", resp.StatusCode)
	}
	return envelope.Project.ProjectNumber, nil
}

// SetProjectStatus sets a project's lifecycle status. Used by teardown to
// mark E2E projects COMPLETED (Fortnox does not allow deleting projects).
func (c *Client) SetProjectStatus(projectNumber, status string) error {
	url := fmt.Sprintf("%s/3/projects/%s", c.baseURL, projectNumber)
	b, _ := json.Marshal(map[string]any{"Project": map[string]any{"Status": status}})
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("set project %s status=%s: %w", projectNumber, status, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set project %s status=%s: status %d", projectNumber, status, resp.StatusCode)
	}
	return nil
}

// ProjectSummary is a minimal project record returned by list operations.
type ProjectSummary struct {
	ProjectNumber string
	Description   string
	Status        string
}

// ListProjectsByPrefix returns projects whose Description starts with prefix.
func (c *Client) ListProjectsByPrefix(prefix string) ([]ProjectSummary, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/3/projects", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("GET projects: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Projects []struct {
			ProjectNumber string `json:"ProjectNumber"`
			Description   string `json:"Description"`
			Status        string `json:"Status"`
		} `json:"Projects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode projects: %w", err)
	}

	var result []ProjectSummary
	for _, p := range envelope.Projects {
		if strings.HasPrefix(p.Description, prefix) {
			result = append(result, ProjectSummary{
				ProjectNumber: p.ProjectNumber,
				Description:   p.Description,
				Status:        p.Status,
			})
		}
	}
	return result, nil
}

// CostCenterCreate holds the fields needed to create a cost center.
type CostCenterCreate struct {
	Code        string // user-defined identifier, e.g. "ENG"
	Description string
}

// CreateCostCenter creates a cost center.
func (c *Client) CreateCostCenter(cc CostCenterCreate) error {
	b, _ := json.Marshal(map[string]any{
		"CostCenter": map[string]any{
			"Code":        cc.Code,
			"Description": cc.Description,
		},
	})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/costcenters", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("POST costcenter: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST costcenter %s: status %d", cc.Code, resp.StatusCode)
	}
	return nil
}

// SetCostCenterActive toggles a cost center's Active flag. Used by teardown.
func (c *Client) SetCostCenterActive(code string, active bool) error {
	url := fmt.Sprintf("%s/3/costcenters/%s", c.baseURL, code)
	b, _ := json.Marshal(map[string]any{"CostCenter": map[string]any{"Active": active}})
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("set costcenter %s active=%v: %w", code, active, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set costcenter %s active=%v: status %d", code, active, resp.StatusCode)
	}
	return nil
}

// CostCenterSummary is a minimal cost center record returned by list operations.
type CostCenterSummary struct {
	Code        string
	Description string
	Active      bool
}

// ListCostCentersByPrefix returns cost centers whose Description starts with
// prefix. Cost-center Code is length-limited (~5 chars) in Fortnox, so it
// can't carry the E2E- marker — the marker lives on Description instead.
func (c *Client) ListCostCentersByPrefix(prefix string) ([]CostCenterSummary, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/3/costcenters", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("GET costcenters: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		CostCenters []struct {
			Code        string `json:"Code"`
			Description string `json:"Description"`
			Active      bool   `json:"Active"`
		} `json:"CostCenters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode costcenters: %w", err)
	}

	var result []CostCenterSummary
	for _, cc := range envelope.CostCenters {
		if strings.HasPrefix(cc.Description, prefix) {
			result = append(result, CostCenterSummary{
				Code:        cc.Code,
				Description: cc.Description,
				Active:      cc.Active,
			})
		}
	}
	return result, nil
}

// AssetCreate holds the fields needed to create a fixed asset.
//
// Required by Fortnox: Number (asset number, free string), Description,
// TypeId (numeric ID from /3/assets/types — e.g. "10" = Datorer/Computers),
// AcquisitionDate, AcquisitionStart (must be the 1st of a month — this is
// the depreciation-start date, mistranslated by Fortnox as "Avskrivningsstart"
// in error messages), AcquisitionValue, DepreciationFinal.
type AssetCreate struct {
	Number            string // user-supplied asset number
	Description       string
	TypeID            string  // ID from /3/assets/types
	AcquisitionDate   string  // YYYY-MM-DD
	AcquisitionStart  string  // YYYY-MM-01 (depreciation start, must be 1st of month)
	AcquisitionValue  float64 // SEK
	DepreciationFinal string  // YYYY-MM-DD — when fully depreciated
}

// CreateAsset creates a fixed asset and returns its assigned Number.
func (c *Client) CreateAsset(a AssetCreate) (string, error) {
	fields := map[string]any{
		"Number":            a.Number,
		"Description":       a.Description,
		"TypeId":            a.TypeID,
		"AcquisitionDate":   a.AcquisitionDate,
		"AcquisitionStart":  a.AcquisitionStart,
		"AcquisitionValue":  a.AcquisitionValue,
		"DepreciationFinal": a.DepreciationFinal,
	}
	b, _ := json.Marshal(map[string]any{"Asset": fields})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/assets", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("POST asset: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Fortnox quirk: the request wrapper is "Asset" (singular) but the
	// response wrapper for POST /3/assets is "Assets" (plural).
	var envelope struct {
		Assets struct {
			Number string `json:"Number"`
		} `json:"Assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("decode asset response: %w", err)
	}
	if envelope.Assets.Number == "" {
		return "", fmt.Errorf("POST asset: status %d", resp.StatusCode)
	}
	return envelope.Assets.Number, nil
}

// AssetSummary is a minimal asset record returned by list operations.
type AssetSummary struct {
	Number      string
	Description string
}

// ListAssetsByPrefix returns assets whose Description starts with prefix.
func (c *Client) ListAssetsByPrefix(prefix string) ([]AssetSummary, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/3/assets", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("GET assets: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Assets []struct {
			Number      string `json:"Number"`
			Description string `json:"Description"`
		} `json:"Assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode assets: %w", err)
	}

	var result []AssetSummary
	for _, a := range envelope.Assets {
		if strings.HasPrefix(a.Description, prefix) {
			result = append(result, AssetSummary{Number: a.Number, Description: a.Description})
		}
	}
	return result, nil
}
