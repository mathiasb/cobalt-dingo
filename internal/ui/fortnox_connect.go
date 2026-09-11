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

	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// ModeStatus holds the connection state for one Fortnox mode.
// Used by FortnoxStatusPage to render mode cards.
type ModeStatus struct {
	Mode      config.Mode
	Connected bool
}

// CompanyChoice is one company the user has connected, for the switcher.
type CompanyChoice struct {
	Name      string
	OrgNumber string
	Active    bool
}

// FortnoxConnector handles the web-based Fortnox OAuth flow for a logged-in user.
// Each mode (sandbox, production) has its own Fortnox connected-app credentials,
// and each company authorised within a mode is a separate connection.
type FortnoxConnector struct {
	configs    map[config.Mode]config.Fortnox
	tokenStore domain.TokenStore
	tenantRepo domain.TenantRepository

	// sessions re-issues the session cookie when the active company changes.
	// Without it a selection could not outlive the request that made it.
	sessions *auth.SessionManager

	log *slog.Logger
}

// NewFortnoxConnector creates a connector. configs maps each supported mode to its
// Fortnox OAuth credentials. modes without an entry in configs will show an error.
func NewFortnoxConnector(
	configs map[config.Mode]config.Fortnox,
	tokenStore domain.TokenStore,
	tenantRepo domain.TenantRepository,
	sessions *auth.SessionManager,
	log *slog.Logger,
) *FortnoxConnector {
	return &FortnoxConnector{
		configs:    configs,
		tokenStore: tokenStore,
		tenantRepo: tenantRepo,
		sessions:   sessions,
		log:        log,
	}
}

// RegisterRoutes wires the connect/callback/status endpoints onto mux.
func (c *FortnoxConnector) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /fortnox/", c.pageHandler)
	mux.HandleFunc("GET /fortnox/connect", c.connectHandler)
	mux.HandleFunc("GET /fortnox/callback", c.callbackHandler)
	mux.HandleFunc("GET /fortnox/status", c.statusHandler)
	mux.HandleFunc("POST /fortnox/disconnect", c.disconnectHandler)
	mux.HandleFunc("POST /fortnox/company", c.selectCompanyHandler)
}

// pageHandler serves GET /fortnox/ — the Fortnox connection management page.
func (c *FortnoxConnector) pageHandler(w http.ResponseWriter, r *http.Request) {
	sess := auth.FromContext(r)
	if sess == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	orderedModes := []config.Mode{config.ModeSandbox, config.ModeProduction}
	var statuses []ModeStatus
	for _, mode := range orderedModes {
		if _, ok := c.configs[mode]; !ok {
			continue
		}
		tid := domain.TenantID(sess.Sub + ":" + string(mode))
		_, err := c.tokenStore.Load(r.Context(), tid)
		statuses = append(statuses, ModeStatus{Mode: mode, Connected: err == nil})
	}

	var flash string
	if m := r.URL.Query().Get("connected"); m != "" {
		if mode := config.Mode(m); mode.IsValid() {
			flash = "Successfully connected to " + mode.Label() + "."
		}
	} else if m := r.URL.Query().Get("disconnected"); m != "" {
		if mode := config.Mode(m); mode.IsValid() {
			flash = "Disconnected from " + mode.Label() + "."
		}
	}

	render(w, r, FortnoxStatusPage(statuses, c.connectedCompanies(r, sess), flash, userNavFrom(r)))
}

// disconnectHandler handles POST /fortnox/disconnect.
func (c *FortnoxConnector) disconnectHandler(w http.ResponseWriter, r *http.Request) {
	sess := auth.FromContext(r)
	if sess == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	mode := config.Mode(r.FormValue("mode"))
	if !mode.IsValid() {
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}
	if _, ok := c.configs[mode]; !ok {
		http.Error(w, fmt.Sprintf("no config for mode %s", mode), http.StatusBadRequest)
		return
	}
	tenantID := domain.TenantID(sess.Sub + ":" + string(mode))
	if err := c.tokenStore.Delete(r.Context(), tenantID); err != nil {
		c.log.Error("delete fortnox token", "tenant", tenantID, "err", err)
		http.Error(w, "disconnect failed", http.StatusInternalServerError)
		return
	}
	c.log.Info("fortnox disconnected", "tenant", tenantID, "mode", mode)
	http.Redirect(w, r, "/fortnox/?disconnected="+string(mode), http.StatusSeeOther)
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
	tok, err := exchangeFortnoxCodeFunc(r.Context(), cfg, code)
	if err != nil {
		c.log.Error("fortnox token exchange failed", "mode", mode, "err", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}

	// Ask Fortnox which company this token is for. Only Fortnox knows: the user
	// approved a company in Fortnox's own UI, and nothing in the callback says
	// which one. Without it the token would be keyed by user and mode alone —
	// which is what made connecting a second company overwrite the first.
	company, err := discoverCompanyFunc(r.Context(), cfg, tok.AccessToken)
	if err != nil {
		c.log.Error("identify fortnox company", "mode", mode, "err", err)
		http.Error(w, "connected, but Fortnox did not say which company — nothing was stored, please retry", http.StatusBadGateway)
		return
	}
	companyKey := auth.CompanyKey(company.OrgNumber)
	if companyKey == "" {
		c.log.Error("fortnox company has no usable organisation number", "mode", mode, "name", company.Name)
		http.Error(w, "connected, but the company has no usable organisation number — nothing was stored", http.StatusBadGateway)
		return
	}

	tenantID := domain.TenantID(sess.Sub + ":" + string(mode) + ":" + companyKey)

	// Ensure tenant row exists before storing token (FK constraint). Named
	// after the company rather than the user's email address: the row exists to
	// say which company this is, and an email address does not.
	if c.tenantRepo != nil {
		if err := c.tenantRepo.UpsertTenant(r.Context(), domain.Tenant{
			ID:   tenantID,
			Name: company.Name,
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
	http.Redirect(w, r, "/fortnox/?connected="+string(mode), http.StatusSeeOther)
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

var exchangeFortnoxCodeFunc = exchangeFortnoxCode

// discoverCompanyFunc is the seam for identifying the company behind a freshly
// issued token. Overridden in tests.
var discoverCompanyFunc = discoverCompany

// discoverCompany reads /3/companyinformation with the new access token.
//
// It cannot go through CompanyInfoAdapter, which loads the token from the store
// by tenant ID — and at this point the token is not stored, because the tenant
// ID is what we are trying to work out. The raw client takes a bare token,
// which is exactly the shape needed here.
//
// readOnly is true: identifying a company must never be able to write to it.
func discoverCompany(_ context.Context, cfg config.Fortnox, accessToken string) (domain.Company, error) {
	row, err := rawfortnox.NewClient(cfg.BaseURL(), accessToken, true).GetCompanyInfo()
	if err != nil {
		return domain.Company{}, fmt.Errorf("read company information: %w", err)
	}
	return domain.Company{Name: row.CompanyName, OrgNumber: row.OrganizationNumber}, nil
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

// connectedCompanies lists the companies this user has connected in the
// session's active mode, marking the active one.
//
// Scoped to the mode on purpose: the same company in sandbox and in production
// are different connections, and offering a sandbox company while production
// is active would invite switching into the wrong books.
//
// Returns nothing rather than failing when there is no tenant repository —
// without a database there is no registry to read, and the page still has to
// render so the user can connect their first company.
func (c *FortnoxConnector) connectedCompanies(r *http.Request, sess *auth.Session) []CompanyChoice {
	if c.tenantRepo == nil {
		return nil
	}
	prefix := sess.Sub + ":" + string(sess.Mode) + ":"
	tenants, err := c.tenantRepo.ListByPrefix(r.Context(), prefix)
	if err != nil {
		c.log.Error("list connected companies", "prefix", prefix, "err", err)
		return nil
	}

	active := auth.CompanyKey(sess.Company)
	out := make([]CompanyChoice, 0, len(tenants))
	for _, t := range tenants {
		key := strings.TrimPrefix(string(t.ID), prefix)
		if key == string(t.ID) {
			continue // not under this prefix; ListByPrefix should not return it
		}
		out = append(out, CompanyChoice{Name: t.Name, OrgNumber: key, Active: key == active})
	}
	return out
}

// selectCompanyHandler switches the company the session is working with.
//
// It verifies a token exists for the requested company before accepting it.
// Without that check a crafted form value would select any key, and the app
// would then try to operate on books it holds no token for — failing later, in
// a place that does not explain why.
func (c *FortnoxConnector) selectCompanyHandler(w http.ResponseWriter, r *http.Request) {
	sess := auth.FromContext(r)
	if sess == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	if c.sessions == nil {
		http.Error(w, "session manager unavailable", http.StatusInternalServerError)
		return
	}

	company := auth.CompanyKey(r.FormValue("company"))
	if company == "" {
		http.Error(w, "no company given", http.StatusBadRequest)
		return
	}

	tenantID := domain.TenantID(sess.Sub + ":" + string(sess.Mode) + ":" + company)
	if _, err := c.tokenStore.Load(r.Context(), tenantID); err != nil {
		c.log.Warn("company selection refused: no token", "tenant", tenantID)
		http.Error(w, "that company is not connected in this mode", http.StatusBadRequest)
		return
	}

	updated := *sess
	updated.Company = company
	if err := c.sessions.Set(w, updated); err != nil {
		c.log.Error("persist company selection", "tenant", tenantID, "err", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	c.log.Info("company selected", "tenant", tenantID)
	http.Redirect(w, r, "/fortnox/", http.StatusSeeOther)
}
