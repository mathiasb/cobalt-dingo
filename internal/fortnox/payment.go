package fortnox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// SupplierInvoicePayment is the request body for recording a payment on a
// supplier invoice. CurrencyRate is the actual execution rate (SEK per 1 FCY).
type SupplierInvoicePayment struct {
	InvoiceNumber int     `json:"InvoiceNumber"`
	Amount        float64 `json:"Amount"`
	CurrencyRate  float64 `json:"CurrencyRate"`
	PaymentDate   string  `json:"PaymentDate"` // YYYY-MM-DD
}

// supplierInvoicePaymentEnvelope wraps the Fortnox API request.
type supplierInvoicePaymentEnvelope struct {
	SupplierInvoicePayment SupplierInvoicePayment `json:"SupplierInvoicePayment"`
}

// RecordPayment creates a supplier invoice payment in Fortnox at the given
// execution rate. The Fortnox payment ID returned can be used to bookkeep.
func (c *Client) RecordPayment(p SupplierInvoicePayment) (int, error) {
	body, err := json.Marshal(supplierInvoicePaymentEnvelope{SupplierInvoicePayment: p})
	if err != nil {
		return 0, fmt.Errorf("marshal payment: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/3/supplierinvoicepayments", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("POST supplierinvoicepayments: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("POST supplierinvoicepayments: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		SupplierInvoicePayment struct {
			Number int `json:"Number"`
		} `json:"SupplierInvoicePayment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode payment response: %w", err)
	}
	return result.SupplierInvoicePayment.Number, nil
}

// BookkeepPayment calls the bookkeep action for a supplier invoice payment,
// which posts the voucher to the GL. Must be called after RecordPayment.
func (c *Client) BookkeepPayment(paymentNumber int) error {
	url := fmt.Sprintf("%s/3/supplierinvoicepayments/%d/bookkeep", c.baseURL, paymentNumber)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT bookkeep %d: %w", paymentNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PUT bookkeep %d: unexpected status %d", paymentNumber, resp.StatusCode)
	}
	return nil
}
