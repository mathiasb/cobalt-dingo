package fortnox

import (
	"encoding/json"
	"fmt"
)

// VoucherRowJSON is a single line in a Fortnox journal entry.
type VoucherRowJSON struct {
	Account                int     `json:"Account"`
	Debit                  float64 `json:"Debit"`
	Credit                 float64 `json:"Credit"`
	TransactionInformation string  `json:"TransactionInformation"`
	CostCenter             string  `json:"CostCenter"`
	Project                string  `json:"Project"`
}

// VoucherJSON is the Fortnox JSON representation of a complete journal entry.
type VoucherJSON struct {
	VoucherSeries   string           `json:"VoucherSeries"`
	VoucherNumber   int              `json:"VoucherNumber"`
	Description     string           `json:"Description"`
	TransactionDate string           `json:"TransactionDate"`
	Year            int              `json:"Year"`
	VoucherRows     []VoucherRowJSON `json:"VoucherRows"`
}

// vouchersResponse is the envelope for GET /3/vouchers.
type vouchersResponse struct {
	Vouchers []VoucherJSON `json:"Vouchers"`
}

// voucherResponse is the envelope for GET /3/vouchers/{series}/{number}.
type voucherResponse struct {
	Voucher VoucherJSON `json:"Voucher"`
}

// ListVouchers returns all vouchers for the given financial year.
// Calls GET /3/vouchers?financialyear={yearID}.
func (c *Client) ListVouchers(yearID int) ([]VoucherJSON, error) {
	u := fmt.Sprintf("%s/3/vouchers?financialyear=%d", c.baseURL, yearID)
	raw, err := c.Get(u)
	if err != nil {
		return nil, fmt.Errorf("list vouchers: %w", err)
	}
	var envelope vouchersResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode vouchers: %w", err)
	}
	return envelope.Vouchers, nil
}

// GetVoucher fetches a single journal entry by series and number.
// Calls GET /3/vouchers/{series}/{number}.
func (c *Client) GetVoucher(series string, number int) (VoucherJSON, error) {
	u := fmt.Sprintf("%s/3/vouchers/%s/%d", c.baseURL, series, number)
	raw, err := c.Get(u)
	if err != nil {
		return VoucherJSON{}, fmt.Errorf("get voucher %s/%d: %w", series, number, err)
	}
	var envelope voucherResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return VoucherJSON{}, fmt.Errorf("decode voucher %s/%d: %w", series, number, err)
	}
	return envelope.Voucher, nil
}

// PredefinedAccountRow maps a system role name to a GL account number.
type PredefinedAccountRow struct {
	Name    string `json:"Name"`
	Account int    `json:"Account"`
}

// predefinedAccountsResponse is the envelope for GET /3/predefinedaccounts.
type predefinedAccountsResponse struct {
	PreDefinedAccounts []PredefinedAccountRow `json:"PreDefinedAccounts"`
}

// ListPredefinedAccounts returns all system-role-to-account mappings.
// Calls GET /3/predefinedaccounts.
func (c *Client) ListPredefinedAccounts() ([]PredefinedAccountRow, error) {
	u := c.baseURL + "/3/predefinedaccounts"
	raw, err := c.Get(u)
	if err != nil {
		return nil, fmt.Errorf("list predefined accounts: %w", err)
	}
	var envelope predefinedAccountsResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode predefined accounts: %w", err)
	}
	return envelope.PreDefinedAccounts, nil
}
