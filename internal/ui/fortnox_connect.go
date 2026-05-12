package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// FortnoxConnector handles the web-based Fortnox OAuth flow for a logged-in user.
// Each mode (sandbox, real_readonly) has its own Fortnox connected-app credentials.
type FortnoxConnector struct {
	configs    map[config.Mode]config.Fortnox
	tokenStore domain.TokenStore
	tenantRepo domain.TenantRepository
	log        *slog.Logger
}

// NewFortnoxConnector creates a connector. configs maps each supported mode to its
// Fortnox OAuth credentials. modes without an entry in configs will show an error.
func NewFortnoxConnector(
	configs map[config.Mode]config.Fortnox,
	tokenStore domain.TokenStore,
	tenantRepo domain.TenantRepository,
	log *slog.Logger,
) *FortnoxConnector {
	return &FortnoxConnector{
		configs:    configs,
		tokenStore: tokenStore,
		tenantRepo: tenantRepo,
		log:        log,
	}
}

// RegisterRoutes wires the connect/callback/status endpoints onto mux.
func (c *FortnoxConnector) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /fortnox/connect", c.connectHandler)
	mux.HandleFunc("GET /fortnox/callback", c.callbackHandler)
	mux.HandleFunc("GET /fortnox/status", c.statusHandler)
}

// connectHandler starts the Fortnox OAuth dance for the session's active mode.
// Query param ?mode= overrides the session mode for this connection only.
func (c *FortnoxConnector) connectHandler(w http.ResponseWriter, r *http.Request) {
	sess := auth.FromContext(r)
	if sess == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	mode := sess.Mode
	if m := config.Mode(r.URL.Query().Get("mode")); m.IsValid() {
		mode = m
	}
	cfg, ok := c.configs[mode]
	if !ok {
		http.Error(w, fmt.Sprintf("no Fortnox config for mode %s", mode), http.StatusBadRequest)
		return
	}

	// Fortnox OAuth2 authorization endpoint.
	params := url.Values{
		"client_id":     {cfg.ClientID},
		"redirect_uri":  {cfg.RedirectURI},
		"scope":         {cfg.Scopes},
		"response_type": {"code"},
		"state":         {string(mode)}, // mode used as state to route callback
		"access_type":   {"offline"},
	}
	authURL := "https://apps.fortnox.se/oauth-v1/auth?" + params.Encode()
	http.Redirect(w, r, authURL, http.StatusFound)
}

// callbackHandler receives the Fortnox auth code, exchanges it for tokens,
// and stores them keyed by (sub:mode) in the token store.
func (c *FortnoxConnector) callbackHandler(w http.ResponseWriter, r *http.Request) {
	sess := auth.FromContext(r)
	if sess == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	mode := config.Mode(r.URL.Query().Get("state"))
	if !mode.IsValid() {
		http.Error(w, "invalid state/mode", http.StatusBadRequest)
		return
	}
	cfg, ok := c.configs[mode]
	if !ok {
		http.Error(w, fmt.Sprintf("no config for mode %s", mode), http.StatusBadRequest)
		return
	}

	// Exchange code for tokens via Fortnox token endpoint.
	tok, err := exchangeFortnoxCode(r.Context(), cfg, code)
	if err != nil {
		c.log.Error("fortnox token exchange failed", "mode", mode, "err", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}

	tenantID := domain.TenantID(sess.Sub + ":" + string(mode))

	// Ensure tenant row exists before storing token (FK constraint).
	if c.tenantRepo != nil {
		if err := c.tenantRepo.UpsertTenant(r.Context(), domain.Tenant{
			ID:   tenantID,
			Name: sess.Email,
		}); err != nil {
			c.log.Error("upsert tenant", "tenant", tenantID, "err", err)
			http.Error(w, "tenant setup failed", http.StatusInternalServerError)
			return
		}
	}

	if err := c.tokenStore.Save(r.Context(), tenantID, tok); err != nil {
		c.log.Error("save fortnox token", "tenant", tenantID, "err", err)
		http.Error(w, "token save failed", http.StatusInternalServerError)
		return
	}

	c.log.Info("fortnox connected", "tenant", tenantID, "mode", mode)
	http.Redirect(w, r, "/", http.StatusFound)
}

// statusHandler returns JSON with which modes have active tokens for the session user.
func (c *FortnoxConnector) statusHandler(w http.ResponseWriter, r *http.Request) {
	sess := auth.FromContext(r)
	if sess == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var parts []string
	for mode := range c.configs {
		tid := domain.TenantID(sess.Sub + ":" + string(mode))
		_, err := c.tokenStore.Load(r.Context(), tid)
		connected := err == nil
		parts = append(parts, fmt.Sprintf(`%q:%v`, mode, connected))
	}
	_, _ = fmt.Fprintf(w, "{%s}", strings.Join(parts, ","))
}

// exchangeFortnoxCode performs the OAuth2 code-for-token exchange with Fortnox.
func exchangeFortnoxCode(ctx context.Context, cfg config.Fortnox, code string) (domain.OAuthToken, error) {
	params := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {cfg.RedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://apps.fortnox.se/oauth-v1/token",
		strings.NewReader(params.Encode()))
	if err != nil {
		return domain.OAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(cfg.ClientID, cfg.ClientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return domain.OAuthToken{}, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return domain.OAuthToken{}, fmt.Errorf("fortnox returned %s", resp.Status)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return domain.OAuthToken{}, fmt.Errorf("decode: %w", err)
	}
	return domain.OAuthToken{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}
