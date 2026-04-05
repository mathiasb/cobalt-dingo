package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// VoucherRow is a single debet/kredit row in a voucher.
type VoucherRow struct {
	Account     int     `json:"Account"`
	Debit       float64 `json:"Debit"`
	Credit      float64 `json:"Credit"`
	Description string  `json:"TransactionInformation,omitempty"`
	VATCode     string  `json:"VATCode,omitempty"`
}

// Voucher represents a Fortnox accounting voucher (verifikation).
type Voucher struct {
	VoucherSeries string       `json:"VoucherSeries,omitempty"`
	VoucherNumber int          `json:"VoucherNumber,omitempty"`
	Description   string       `json:"Description"`
	VoucherDate   string       `json:"VoucherDate"` // YYYY-MM-DD
	Rows          []VoucherRow `json:"VoucherRows"`
}

type voucherRequest struct {
	Voucher Voucher `json:"Voucher"`
}

type voucherResponse struct {
	Voucher Voucher `json:"Voucher"`
}

type vouchersResponse struct {
	Vouchers []Voucher `json:"Vouchers"`
}

// ListVouchers returns vouchers for the given financial year.
func (c *Client) ListVouchers(ctx context.Context, fromDate, toDate time.Time) ([]Voucher, error) {
	path := fmt.Sprintf("vouchers?fromdate=%s&todate=%s",
		fromDate.Format("2006-01-02"),
		toDate.Format("2006-01-02"),
	)
	var resp vouchersResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("api: list vouchers: %w", err)
	}
	return resp.Vouchers, nil
}

// CreateVoucher posts a new voucher to Fortnox.
// Requires explicit user confirmation before calling – gul säkerhetsnivå.
func (c *Client) CreateVoucher(ctx context.Context, v Voucher) (*Voucher, error) {
	body, err := json.Marshal(voucherRequest{Voucher: v})
	if err != nil {
		return nil, fmt.Errorf("api: marshal voucher: %w", err)
	}

	url := c.baseURL + "vouchers"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("api: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if err := c.setAuthHeader(ctx, req); err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: create voucher request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	_ = c.cfg.AuditLog.Log(audit_entry("POST", "vouchers", resp.StatusCode, time.Since(start)))

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("api: create voucher returned %d: %s", resp.StatusCode, respBody)
	}

	var out voucherResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("api: unmarshal voucher response: %w", err)
	}
	return &out.Voucher, nil
}
