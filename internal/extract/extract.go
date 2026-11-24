// Package extract reads the fields of a supplier invoice from an image using a
// vision-language model served by an Ollama-compatible endpoint.
//
// It only reads: the image goes to the model, a typed Invoice comes back.
// Nothing is written to any ledger. The model is local by design (iguana);
// pointing BaseURL at a cloud API would send the document off the network,
// which the bet's guardrail forbids.
package extract

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const prompt = "Extract from this invoice image as JSON with exactly these keys: " +
	"supplier, invoice_number, invoice_date (YYYY-MM-DD), due_date (YYYY-MM-DD), " +
	"currency (ISO code), total (amount to pay, plain decimal with dot), " +
	"vat_amount (plain decimal with dot). Use null if absent."

// Invoice is what the model read from the image. Amounts are minor units.
// DueDate is empty when the invoice states none.
type Invoice struct {
	Supplier      string
	InvoiceNumber string
	InvoiceDate   string // YYYY-MM-DD
	DueDate       string // YYYY-MM-DD
	Currency      string // ISO 4217
	Total         int64
	VATAmount     int64
}

// Client talks to an Ollama-compatible /api/chat endpoint.
type Client struct {
	BaseURL string
	Model   string
	HTTP    *http.Client // optional; defaults to a client with a 5 minute timeout
}

// Extract sends the image to the model and parses its reply. A reply that is
// not valid JSON, or lacks or garbles a field, is an error naming the field;
// partial data is never returned as a result.
func (c *Client) Extract(ctx context.Context, image []byte) (Invoice, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model":   c.Model,
		"stream":  false,
		"format":  "json",
		"options": map[string]any{"temperature": 0},
		"messages": []map[string]any{{
			"role":    "user",
			"content": prompt,
			"images":  []string{base64.StdEncoding.EncodeToString(image)},
		}},
	})
	if err != nil {
		return Invoice{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/api/chat", bytes.NewReader(reqBody))
	if err != nil {
		return Invoice{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Invoice{}, fmt.Errorf("call model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Invoice{}, fmt.Errorf("read model reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Invoice{}, fmt.Errorf("model returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Invoice{}, fmt.Errorf("parse model envelope as json: %w", err)
	}
	return parseInvoice(envelope.Message.Content)
}

// amountText accepts a JSON string or a JSON number.
type amountText string

func (a *amountText) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		return nil
	}
	s = strings.Trim(s, `"`)
	*a = amountText(s)
	return nil
}

func parseInvoice(content string) (Invoice, error) {
	var raw struct {
		Supplier      *string    `json:"supplier"`
		InvoiceNumber *string    `json:"invoice_number"`
		InvoiceDate   *string    `json:"invoice_date"`
		DueDate       *string    `json:"due_date"`
		Currency      *string    `json:"currency"`
		Total         amountText `json:"total"`
		VATAmount     amountText `json:"vat_amount"`
	}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return Invoice{}, fmt.Errorf("model reply is not json: %w", err)
	}

	var inv Invoice
	for _, f := range []struct {
		name string
		src  *string
		dst  *string
	}{
		{"supplier", raw.Supplier, &inv.Supplier},
		{"invoice_number", raw.InvoiceNumber, &inv.InvoiceNumber},
		{"invoice_date", raw.InvoiceDate, &inv.InvoiceDate},
		{"currency", raw.Currency, &inv.Currency},
	} {
		if f.src == nil || strings.TrimSpace(*f.src) == "" {
			return Invoice{}, fmt.Errorf("field %s is missing", f.name)
		}
		*f.dst = strings.TrimSpace(*f.src)
	}
	// The due date is optional: many real invoices carry none and the booking
	// proposal does not need it. A due date that is present must still parse.
	if raw.DueDate != nil && strings.TrimSpace(*raw.DueDate) != "" {
		inv.DueDate = strings.TrimSpace(*raw.DueDate)
	}
	for _, d := range []struct {
		name     string
		v        string
		optional bool
	}{{"invoice_date", inv.InvoiceDate, false}, {"due_date", inv.DueDate, true}} {
		if d.optional && d.v == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", d.v); err != nil {
			return Invoice{}, fmt.Errorf("field %s is not YYYY-MM-DD: %q", d.name, d.v)
		}
	}
	if len(inv.Currency) != 3 || strings.ToUpper(inv.Currency) != inv.Currency {
		return Invoice{}, fmt.Errorf("field currency is not an ISO code: %q", inv.Currency)
	}

	var err error
	if inv.Total, err = ParseAmount(string(raw.Total)); err != nil {
		return Invoice{}, fmt.Errorf("field total: %w", err)
	}
	if inv.VATAmount, err = ParseAmount(string(raw.VATAmount)); err != nil {
		return Invoice{}, fmt.Errorf("field vat_amount: %w", err)
	}
	return inv, nil
}

// ParseAmount reads a decimal amount into minor units without floating point.
// A comma is accepted as the decimal mark and spaces as thousands separators;
// more than two decimals is an error rather than a silent rounding.
func ParseAmount(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, errors.New("amount is empty")
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > 2 {
		return 0, fmt.Errorf("amount %q is not a decimal with at most two places", s)
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("amount %q is not a number", s)
		}
	}
	for len(frac) < 2 {
		frac += "0"
	}
	var n int64
	for _, r := range whole + frac {
		n = n*10 + int64(r-'0')
	}
	return n, nil
}
