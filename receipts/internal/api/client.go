package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/mathiasb/coo-agent/internal/auth"
	"github.com/mathiasb/coo-agent/internal/validator"
)

const (
	defaultBaseURL    = "https://api.fortnox.se/3/"
	defaultRefreshURL = "https://api.fortnox.se/oauth-v1/token"
	maxRetries        = 3
	retryBaseDelay    = 100 * time.Millisecond
)

// Client is a Fortnox REST API client. It implements FortnoxClient.
type Client struct {
	cfg        Config
	httpClient *http.Client
	baseURL    string
	refreshURL string
}

// New creates a new Client. BaseURL may be overridden for tests.
func New(cfg Config) (*Client, error) {
	if cfg.ClientID == "" {
		return nil, errors.New("api: ClientID is required")
	}
	if cfg.TokenStore == nil {
		return nil, errors.New("api: TokenStore is required")
	}
	if cfg.AuditLog == nil {
		cfg.AuditLog = audit.NopLogger{}
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
		if cfg.Sandbox {
			slog.Info("Fortnox client running in SANDBOX mode")
		}
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	refreshURL := cfg.TokenRefreshURL
	if refreshURL == "" {
		refreshURL = defaultRefreshURL
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    base,
		refreshURL: refreshURL,
	}, nil
}

// ListInvoices returns invoices optionally filtered by status (e.g. "unpaid").
func (c *Client) ListInvoices(ctx context.Context, filter string) ([]Invoice, error) {
	path := "invoices"
	if filter != "" {
		path += "?filter=" + filter
	}
	var resp struct {
		Invoices []Invoice `json:"Invoices"`
	}
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("api: list invoices: %w", err)
	}
	return resp.Invoices, nil
}

// ListVouchers returns vouchers created between from and to.
func (c *Client) ListVouchers(ctx context.Context, from, to time.Time) ([]Voucher, error) {
	path := fmt.Sprintf("vouchers?fromdate=%s&todate=%s",
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	var resp struct {
		Vouchers []Voucher `json:"Vouchers"`
	}
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("api: list vouchers: %w", err)
	}
	return resp.Vouchers, nil
}

// ListAccounts returns all chart-of-accounts entries.
func (c *Client) ListAccounts(ctx context.Context) ([]validator.Account, error) {
	var resp struct {
		Accounts []struct {
			Number      int    `json:"Number"`
			Description string `json:"Description"`
		} `json:"Accounts"`
	}
	if err := c.get(ctx, "accounts", &resp); err != nil {
		return nil, fmt.Errorf("api: list accounts: %w", err)
	}
	accounts := make([]validator.Account, len(resp.Accounts))
	for i, a := range resp.Accounts {
		accounts[i] = validator.Account{Number: a.Number, Description: a.Description}
	}
	return accounts, nil
}

// ListSupplierInvoices returns supplier invoices, optionally filtered.
func (c *Client) ListSupplierInvoices(ctx context.Context, filter string) ([]SupplierInvoice, error) {
	path := "supplierinvoices"
	if filter != "" {
		path += "?filter=" + filter
	}
	var resp struct {
		SupplierInvoices []SupplierInvoice `json:"SupplierInvoices"`
	}
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("api: list supplier invoices: %w", err)
	}
	return resp.SupplierInvoices, nil
}

// ListAssets returns all fixed assets from the asset registry.
func (c *Client) ListAssets(ctx context.Context) ([]Asset, error) {
	var resp struct {
		Assets []Asset `json:"Assets"`
	}
	if err := c.get(ctx, "assets", &resp); err != nil {
		return nil, fmt.Errorf("api: list assets: %w", err)
	}
	return resp.Assets, nil
}

// GetCompanyInfo returns basic company information.
func (c *Client) GetCompanyInfo(ctx context.Context) (*CompanyInfo, error) {
	var resp struct {
		CompanyInformation CompanyInfo `json:"CompanyInformation"`
	}
	if err := c.get(ctx, "companyinformation", &resp); err != nil {
		return nil, fmt.Errorf("api: get company info: %w", err)
	}
	return &resp.CompanyInformation, nil
}

// CreateVoucher creates a new accounting voucher. Requires explicit user confirmation.
func (c *Client) CreateVoucher(ctx context.Context, v Voucher) (*Voucher, error) {
	body, err := json.Marshal(map[string]any{"Voucher": v})
	if err != nil {
		return nil, fmt.Errorf("api: marshal voucher: %w", err)
	}
	var resp struct {
		Voucher Voucher `json:"Voucher"`
	}
	if err := c.post(ctx, "vouchers", body, http.StatusCreated, &resp); err != nil {
		return nil, fmt.Errorf("api: create voucher: %w", err)
	}
	return &resp.Voucher, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.doWithRetry(ctx, http.MethodGet, path, nil, http.StatusOK, out)
}

func (c *Client) post(ctx context.Context, path string, body []byte, wantStatus int, out any) error {
	return c.doWithRetry(ctx, http.MethodPost, path, body, wantStatus, out)
}

func (c *Client) doWithRetry(ctx context.Context, method, path string, body []byte, wantStatus int, out any) error {
	url := c.baseURL + path
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			backoff := retryBaseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return fmt.Errorf("api: build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if err := c.attachToken(ctx, req); err != nil {
			return err
		}

		start := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck

		_ = c.cfg.AuditLog.Log(audit.Entry{
			Op:         method,
			Endpoint:   path,
			Status:     resp.StatusCode,
			DurationMs: time.Since(start).Milliseconds(),
		})

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("api: %s %s returned 429", method, path)
			continue
		}
		if resp.StatusCode != wantStatus {
			return fmt.Errorf("api: %s %s returned %d: %s", method, path, resp.StatusCode, respBody)
		}
		return json.Unmarshal(respBody, out)
	}
	return fmt.Errorf("api: %s %s failed after %d attempts: %w", method, path, maxRetries, lastErr)
}

func (c *Client) attachToken(ctx context.Context, req *http.Request) error {
	token, err := c.cfg.TokenStore.Load()
	if err != nil {
		return fmt.Errorf("api: load token: %w", err)
	}
	if token.IsExpired() {
		token, err = c.refreshToken(ctx, token)
		if err != nil {
			return fmt.Errorf("api: refresh token: %w", err)
		}
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	return nil
}

func (c *Client) refreshToken(ctx context.Context, token *auth.Token) (*auth.Token, error) {
	data := fmt.Sprintf(
		"grant_type=refresh_token&refresh_token=%s&client_id=%s&client_secret=%s",
		token.RefreshToken, c.cfg.ClientID, c.cfg.ClientSecret,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.refreshURL,
		strings.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api: token refresh returned %d: %s", resp.StatusCode, body)
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	newToken := &auth.Token{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		TokenType:    result.TokenType,
		Expiry:       time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}
	if err := c.cfg.TokenStore.Save(newToken); err != nil {
		return nil, fmt.Errorf("api: save refreshed token: %w", err)
	}
	return newToken, nil
}
