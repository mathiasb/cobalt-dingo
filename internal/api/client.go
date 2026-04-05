// Package api provides a client for the Fortnox REST API with OAuth2,
// automatic token refresh, retry logic, and audit logging.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/mathiasb/coo-agent/internal/auth"
)

const (
	baseURL        = "https://api.fortnox.se/3/"
	sandboxBaseURL = "https://api.fortnox.se/3/" // same URL, different credentials
	maxRetries     = 3
)

// Config holds client configuration.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Sandbox      bool
	TokenStore   *auth.TokenStore
	AuditLog     *audit.Logger
}

// Client is a Fortnox API client.
type Client struct {
	cfg        Config
	httpClient *http.Client
	baseURL    string
}

// New creates a new Fortnox API client.
func New(cfg Config) (*Client, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("api: ClientID and ClientSecret are required")
	}
	base := baseURL
	if cfg.Sandbox {
		base = sandboxBaseURL
		slog.Info("Fortnox client running in SANDBOX mode")
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    base,
	}, nil
}

// get performs an authenticated GET request with retry and audit logging.
func (c *Client) get(ctx context.Context, path string, out any) error {
	url := c.baseURL + path
	start := time.Now()
	var lastStatus int

	for attempt := range maxRetries {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			slog.Info("retrying Fortnox request", "path", path, "attempt", attempt+1, "backoff", backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("api: build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if err := c.setAuthHeader(ctx, req); err != nil {
			return err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("api: request failed: %w", err)
		}
		lastStatus = resp.StatusCode
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		_ = c.cfg.AuditLog.Log(audit.Entry{
			Op:         "GET",
			Endpoint:   path,
			Status:     resp.StatusCode,
			DurationMs: time.Since(start).Milliseconds(),
		})

		switch {
		case resp.StatusCode == http.StatusOK:
			return json.Unmarshal(body, out)
		case resp.StatusCode == http.StatusTooManyRequests:
			continue // retry with backoff
		case resp.StatusCode == http.StatusServiceUnavailable && attempt < 1:
			time.Sleep(5 * time.Second)
			continue
		default:
			return fmt.Errorf("api: GET %s returned %d: %s", path, resp.StatusCode, body)
		}
	}
	return fmt.Errorf("api: GET %s failed after %d attempts, last status %d", path, maxRetries, lastStatus)
}

// setAuthHeader attaches the Bearer token to the request,
// refreshing it first if it is about to expire.
func (c *Client) setAuthHeader(ctx context.Context, req *http.Request) error {
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

// refreshToken exchanges the refresh token for a new access token.
func (c *Client) refreshToken(_ context.Context, _ *auth.Token) (*auth.Token, error) {
	// TODO: implement token refresh via Fortnox OAuth2 endpoint
	return nil, fmt.Errorf("api: token refresh not yet implemented")
}
