package fortnox

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SupplierRow is the Fortnox JSON representation of a supplier's payment details.
type SupplierRow struct {
	SupplierNumber int    `json:"SupplierNumber"`
	IBAN           string `json:"IBAN"`
	BIC            string `json:"BIC"`
}

// SupplierResponse is the top-level envelope returned by GET /3/suppliers/{num}.
type SupplierResponse struct {
	Supplier SupplierRow `json:"Supplier"`
}

// SupplierPaymentDetails fetches IBAN and BIC for a supplier from Fortnox.
// Returns empty strings when the supplier has no IBAN configured.
func (c *Client) SupplierPaymentDetails(supplierNumber int) (iban, bic string, err error) {
	url := fmt.Sprintf("%s/3/suppliers/%d", c.baseURL, supplierNumber)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("GET supplier %d: %w", supplierNumber, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GET supplier %d: unexpected status %d", supplierNumber, resp.StatusCode)
	}

	var envelope SupplierResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", "", fmt.Errorf("decode supplier %d: %w", supplierNumber, err)
	}
	return envelope.Supplier.IBAN, envelope.Supplier.BIC, nil
}
