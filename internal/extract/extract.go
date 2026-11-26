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
	"supplier, invoice_number, invoice_date (exactly as printed, do not reformat), " +
	"due_date (exactly as printed, do not reformat), " +
	"currency (ISO code), net_total (amount excluding VAT, plain decimal with dot), " +
	"vat_amount (plain decimal with dot), total (amount to pay including VAT, plain decimal with dot). " +
	"Use null if absent."

// totalPrompt is the second read. It asks one narrow question and shows the model
// nothing of the first answer: the broad prompt above was seen to read a discount
// as VAT and invent a total, where this one gave the printed total every time.
const totalPrompt = "What is the final amount the customer has to pay on this invoice, exactly as printed? " +
	`Return JSON {"total_label": "<the words printed next to it>", "total": "<plain decimal with dot>"}.`

// MaxAmountMinor is the largest amount accepted, ten billion in major units.
// A reading near the top of int64 makes sums wrap, and a wrapped sum can pass
// the net + VAT = total check, so an absurd reading is refused instead.
const MaxAmountMinor int64 = 1_000_000_000_000

// maxTextLen bounds, in bytes, the supplier and invoice number the model may return. They
// flow into a page, a journal text and a CSV.
const maxTextLen = 200

// Invoice is what the model read from the image. Amounts are minor units.
// DueDate is empty when the invoice states none.
type Invoice struct {
	Supplier      string
	InvoiceNumber string
	InvoiceDate   string // YYYY-MM-DD
	DueDate       string // YYYY-MM-DD

	// InvoiceDateAmbiguous and DueDateAmbiguous are true when the printed date
	// could be read two ways and the client's DateOrder chose one.
	InvoiceDateAmbiguous bool
	DueDateAmbiguous     bool
	Currency             string // ISO 4217
	Total                int64
	VATAmount            int64

	// NetTotal is the amount excluding VAT. NetKnown is false when the model
	// did not read one; the invoice then cannot be cross-checked.
	NetTotal int64
	NetKnown bool

	// TotalDisputed is true when a second, independent read of the total did not
	// give the same amount, or gave none. Net + VAT = total can hold while all
	// three are wrong together (a discount read as VAT, with a made-up total), so
	// the arithmetic alone cannot vouch for the total. TotalReread is the second
	// reading, valid only when TotalRereadKnown.
	TotalDisputed    bool
	TotalReread      int64
	TotalRereadKnown bool
}

// Client talks to an Ollama-compatible /api/chat endpoint.
type Client struct {
	BaseURL string
	Model   string
	HTTP    *http.Client // optional; defaults to a client with a 5 minute timeout

	// TextModel reads documents that arrive as text (ExtractText). Empty means
	// Model, the vision model, which also reads text.
	TextModel string

	// DateOrder resolves numeric dates such as 02.11.2026. The zero value is
	// DMY. It is a setting of the deployment, not something the model guesses.
	DateOrder DateOrder
}

// Extract sends the image to the model and parses its reply. A reply that is
// not valid JSON, or lacks or garbles a field, is an error naming the field;
// partial data is never returned as a result.
func (c *Client) Extract(ctx context.Context, image []byte) (Invoice, error) {
	content, err := c.chat(ctx, c.Model, prompt, []string{base64.StdEncoding.EncodeToString(image)})
	if err != nil {
		return Invoice{}, err
	}
	inv, err := parseInvoice(content, c.DateOrder)
	if err != nil {
		return Invoice{}, err
	}
	c.confirmTotal(ctx, c.Model, totalPrompt, []string{base64.StdEncoding.EncodeToString(image)}, &inv)
	return inv, nil
}

// confirmTotal reads the total a second time, from the same document and with a different
// question, and marks the invoice disputed unless the two readings are equal. A
// second read that fails or cannot be parsed also disputes: an unconfirmed total
// is not a confirmed one.
func (c *Client) confirmTotal(ctx context.Context, model, question string, images []string, inv *Invoice) {
	content, err := c.chat(ctx, model, question, images)
	if err == nil {
		var raw struct {
			Total amountText `json:"total"`
		}
		if json.Unmarshal([]byte(content), &raw) == nil {
			if v, perr := ParseAmount(string(raw.Total)); perr == nil {
				inv.TotalReread, inv.TotalRereadKnown = v, true
			}
		}
	}
	inv.TotalDisputed = !inv.TotalRereadKnown || inv.TotalReread != inv.Total
}

// chat sends one user message to the model and returns the reply's content. The
// document goes to the configured host and nowhere else: redirects are not followed.
func (c *Client) chat(ctx context.Context, model, content string, images []string) (string, error) {
	msg := map[string]any{"role": "user", "content": content}
	if len(images) > 0 {
		msg["images"] = images
	}
	reqBody, err := json.Marshal(map[string]any{
		"model":    model,
		"stream":   false,
		"format":   "json",
		"options":  map[string]any{"temperature": 0},
		"messages": []map[string]any{msg},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/api/chat", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{
			Timeout:       5 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("call model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read model reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("parse model envelope as json: %w", err)
	}
	return envelope.Message.Content, nil
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

func parseInvoice(content string, order DateOrder) (Invoice, error) {
	var raw struct {
		Supplier      *string    `json:"supplier"`
		InvoiceNumber *string    `json:"invoice_number"`
		InvoiceDate   *string    `json:"invoice_date"`
		DueDate       *string    `json:"due_date"`
		Currency      *string    `json:"currency"`
		NetTotal      amountText `json:"net_total"`
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
	d, err := ParseDate(inv.InvoiceDate, order)
	if err != nil {
		return Invoice{}, fmt.Errorf("field invoice_date: %w", err)
	}
	inv.InvoiceDate, inv.InvoiceDateAmbiguous = d.ISO, d.Ambiguous

	// The due date is optional: many real invoices carry none and the booking
	// proposal does not need one. A due date that is present must still parse.
	if raw.DueDate != nil && strings.TrimSpace(*raw.DueDate) != "" {
		dd, err := ParseDate(*raw.DueDate, order)
		if err != nil {
			return Invoice{}, fmt.Errorf("field due_date: %w", err)
		}
		inv.DueDate, inv.DueDateAmbiguous = dd.ISO, dd.Ambiguous
	}
	if !isCurrencyCode(inv.Currency) {
		return Invoice{}, fmt.Errorf("field currency is not an ISO code: %q", inv.Currency)
	}
	for _, f := range []struct{ name, v string }{{"supplier", inv.Supplier}, {"invoice_number", inv.InvoiceNumber}} {
		if len(f.v) > maxTextLen {
			return Invoice{}, fmt.Errorf("field %s is longer than %d bytes", f.name, maxTextLen)
		}
	}

	if inv.Total, err = ParseAmount(string(raw.Total)); err != nil {
		return Invoice{}, fmt.Errorf("field total: %w", err)
	}
	if inv.VATAmount, err = ParseAmount(string(raw.VATAmount)); err != nil {
		return Invoice{}, fmt.Errorf("field vat_amount: %w", err)
	}
	// An unreadable net total is not fatal: the invoice is still returned, but
	// it cannot reconcile, and Warnings says so.
	if n, nerr := ParseAmount(string(raw.NetTotal)); nerr == nil {
		inv.NetTotal, inv.NetKnown = n, true
	}
	return inv, nil
}

// ParseAmount reads a decimal amount into minor units without floating point.
// A single leading minus gives a negative amount (a credit note).
// A comma is accepted as the decimal mark and spaces as thousands separators;
// more than two decimals is an error rather than a silent rounding.
func ParseAmount(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	// One leading minus marks a credit note's amount. Any other sign is not a number.
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
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
		if n > MaxAmountMinor {
			return 0, fmt.Errorf("amount %q is larger than the %d minor units accepted", s, MaxAmountMinor)
		}
	}
	if negative {
		n = -n
	}
	return n, nil
}

func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
