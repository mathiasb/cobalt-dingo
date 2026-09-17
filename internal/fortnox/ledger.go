package fortnox

import (
	"encoding/json"
	"fmt"
)

// VoucherRowJSON is a single line in a Fortnox journal entry.
//
// Faithful to the API shape, including Removed — filtering belongs at the
// domain boundary, not here.
type VoucherRowJSON struct {
	Account                FlexInt `json:"Account"`
	Debit                  float64 `json:"Debit"`
	Credit                 float64 `json:"Credit"`
	TransactionInformation string  `json:"TransactionInformation"`
	CostCenter             string  `json:"CostCenter"`
	Project                string  `json:"Project"`

	// Removed marks a row that was deleted in Fortnox. The detail endpoint
	// returns removed rows alongside live ones, so a reader that ignores this
	// flag counts corrections twice — and the voucher then appears not to
	// balance. Fortnox will not accept an unbalanced voucher through its own
	// UI, so an unbalanced voucher always means we are reading it wrong.
	Removed bool `json:"Removed"`
}

// VoucherJSON is the Fortnox JSON representation of a complete journal entry.
type VoucherJSON struct {
	VoucherSeries   string           `json:"VoucherSeries"`
	VoucherNumber   FlexInt          `json:"VoucherNumber"`
	Description     string           `json:"Description"`
	TransactionDate string           `json:"TransactionDate"`
	Year            FlexInt          `json:"Year"`
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
	// Every page, via the existing helper. Reading page one returned exactly
	// 100 vouchers for a year that holds more, and produced a confident wrong
	// answer about account 1930 (2026-09-14). A truncated list is worse than
	// an error because it is plausible.
	//
	// GetAllPages already existed, with a test and no callers — the gap was
	// never that pagination was unknown here, only that this endpoint did not
	// use it. Adding a second loop instead would have left two implementations
	// to drift apart.
	pages, err := c.GetAllPages(fmt.Sprintf("%s/3/vouchers?financialyear=%d", c.baseURL, yearID))
	if err != nil {
		return nil, fmt.Errorf("list vouchers: %w", err)
	}
	var out []VoucherJSON
	for i, raw := range pages {
		var envelope vouchersResponse
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("decode vouchers page %d: %w", i+1, err)
		}
		out = append(out, envelope.Vouchers...)
	}
	return out, nil
}

// ListVouchersWithRows returns the year's vouchers WITH their rows.
//
// GET /3/vouchers declares VoucherRows in the OpenAPI spec and returns them
// empty; rows arrive only from the per-voucher detail endpoint. Measured on
// live production 2026-09-14: 405 vouchers, 0 with rows. Every row-level
// analysis therefore returned nothing and looked like an answer (#89).
//
// The cost is one request per voucher. At the documented limit — 300 per
// minute, 25 per 5 seconds, and this client's limiter is set to 18 for
// headroom — a 405-voucher year takes roughly two minutes. That is the price
// of a correct answer from this API shape, and it belongs in a scheduled
// snapshot rather than behind an interactive call. Caching the details is the
// recorded follow-up.
//
// A voucher whose detail cannot be read is an ERROR, not an omission. Skipping
// it would return a short list that looks complete, which is the same defect
// this function exists to remove.
func (c *Client) ListVouchersWithRows(yearID int) ([]VoucherJSON, error) {
	heads, err := c.ListVouchers(yearID)
	if err != nil {
		return nil, err
	}
	out := make([]VoucherJSON, 0, len(heads))
	for _, h := range heads {
		full, err := c.GetVoucher(yearID, h.VoucherSeries, int(h.VoucherNumber))
		if err != nil {
			return nil, fmt.Errorf("voucher %s/%d: %w", h.VoucherSeries, int(h.VoucherNumber), err)
		}
		// Keep the list's transaction date: the detail endpoint agrees, but the
		// list is what the caller filtered on, so preferring it keeps the set
		// self-consistent if they ever diverge.
		full.TransactionDate = h.TransactionDate
		out = append(out, full)
	}
	return out, nil
}

// GetVoucher fetches a single journal entry.
// Calls GET /3/vouchers/{series}/{number}?financialyear={yearID}.
//
// yearID is required, not optional. A voucher is identified by (financial
// year, series, number) because numbers restart per series each year, so
// "A/144" names one voucher per year the company has traded. Omitting the
// parameter lets Fortnox answer about a voucher nobody asked for.
//
// Measured on live production 2026-09-14, before this parameter was sent:
// four of 405 vouchers came back carrying rows that were not theirs — a
// complete balanced voucher followed by a partial second one. M/2 balanced
// across its first six rows and then credited account 2650 again, and the
// company's trial balance was SEK 122,881.80 out as a result. Same failure
// shape as reading page one of a paginated list: a plausible answer with
// nothing about it to notice.
func (c *Client) GetVoucher(yearID int, series string, number int) (VoucherJSON, error) {
	u := fmt.Sprintf("%s/3/vouchers/%s/%d?financialyear=%d", c.baseURL, series, number, yearID)
	raw, err := c.get(u)
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
// Fortnox returns Account as a string or number inconsistently across API versions.
type PredefinedAccountRow struct {
	Name    string  `json:"Name"`
	Account FlexInt `json:"Account"`
}

// predefinedAccountsResponse is the envelope for GET /3/predefinedaccounts.
type predefinedAccountsResponse struct {
	PreDefinedAccounts []PredefinedAccountRow `json:"PreDefinedAccounts"`
}

// ListPredefinedAccounts returns all system-role-to-account mappings.
// Calls GET /3/predefinedaccounts.
func (c *Client) ListPredefinedAccounts() ([]PredefinedAccountRow, error) {
	u := c.baseURL + "/3/predefinedaccounts"
	raw, err := c.get(u)
	if err != nil {
		return nil, fmt.Errorf("list predefined accounts: %w", err)
	}
	var envelope predefinedAccountsResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode predefined accounts: %w", err)
	}
	return envelope.PreDefinedAccounts, nil
}

// VoucherTotal returns Fortnox's own voucher count for a financial year.
//
// One request, no pagination: this is the cheap integrity check the voucher
// cache is built on (ADR-0006), and a check that costs as much as the fetch it
// guards is not a check.
//
// Delegates to TotalResources, which holds the absent-vs-zero guard. That
// guard was written here first and then needed again for invoice counts; two
// copies is how it comes back in only one of them.
func (c *Client) VoucherTotal(yearID int) (int, error) {
	total, err := c.TotalResources(fmt.Sprintf("%s/3/vouchers?financialyear=%d", c.baseURL, yearID))
	if err != nil {
		return 0, fmt.Errorf("voucher total for year %d: %w", yearID, err)
	}
	return total, nil
}
