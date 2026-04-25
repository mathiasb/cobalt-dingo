// Package fortnox provides a client for the Fortnox REST API v3.
package fortnox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// rateLimiter enforces the Fortnox limit of 25 requests per 5 seconds.
var rateLimiter = struct {
	mu        sync.Mutex
	count     int
	windowEnd time.Time
}{}

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
func (c *Client) UnpaidSupplierInvoices() ([]domain.SupplierInvoice, error) {
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

	invoices := make([]domain.SupplierInvoice, len(envelope.SupplierInvoices))
	for i, row := range envelope.SupplierInvoices {
		invoices[i] = domain.SupplierInvoice{
			InvoiceNumber:  row.InvoiceNumber,
			SupplierNumber: row.SupplierNumber,
			SupplierName:   row.SupplierName,
			Amount:         domain.MoneyFromFloat(row.TotalInvoiceCurrency, row.Currency),
			DueDate:        row.DueDate,
		}
	}
	return invoices, nil
}

// MetaInformation is the pagination envelope Fortnox includes in list responses.
type MetaInformation struct {
	TotalPages     int `json:"@TotalPages"`
	CurrentPage    int `json:"@CurrentPage"`
	TotalResources int `json:"@TotalResources"`
}

// metaEnvelope is used to extract MetaInformation from a raw response.
type metaEnvelope struct {
	Meta MetaInformation `json:"MetaInformation"`
}

// waitForRate blocks until a request slot is available within the Fortnox
// rate limit (25 requests per 5-second window).
func waitForRate() {
	const maxReqs = 25
	const window = 5 * time.Second

	for {
		rateLimiter.mu.Lock()
		now := time.Now()
		if now.After(rateLimiter.windowEnd) {
			rateLimiter.count = 0
			rateLimiter.windowEnd = now.Add(window)
		}
		if rateLimiter.count < maxReqs {
			rateLimiter.count++
			rateLimiter.mu.Unlock()
			return
		}
		wait := time.Until(rateLimiter.windowEnd)
		rateLimiter.mu.Unlock()
		time.Sleep(wait)
	}
}

// Get performs an authenticated, rate-limited GET request and returns the raw
// JSON response body.
func (c *Client) Get(requestURL string) (json.RawMessage, error) {
	waitForRate()

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", requestURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", requestURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return json.RawMessage(body), nil
}

// GetAllPages fetches every page from a paginated Fortnox endpoint. It sets
// the `page` query parameter on successive requests and reads MetaInformation
// from each response to determine when to stop. One json.RawMessage per page
// is returned.
func (c *Client) GetAllPages(baseURL string) ([]json.RawMessage, error) {
	var pages []json.RawMessage

	for page := 1; ; page++ {
		u, err := url.Parse(baseURL)
		if err != nil {
			return nil, fmt.Errorf("parse URL: %w", err)
		}
		q := u.Query()
		q.Set("page", strconv.Itoa(page))
		u.RawQuery = q.Encode()

		raw, err := c.Get(u.String())
		if err != nil {
			return nil, fmt.Errorf("get page %d: %w", page, err)
		}
		pages = append(pages, raw)

		var env metaEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("decode meta page %d: %w", page, err)
		}

		if page >= env.Meta.TotalPages || env.Meta.TotalPages == 0 {
			break
		}
	}

	return pages, nil
}
