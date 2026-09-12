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

	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/integration"
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

	// Mode is carried so the Disconnect form can name it. Disconnect is
	// per (mode, company); a mode-level button could not say which company it
	// would remove.
	Mode config.Mode
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

	// integrations and cipher resolve per-owner Fortnox credentials (ADR-0005).
	// Both nil means single-tenant: the application-level credentials are used.
	integrations domain.IntegrationStore
	cipher       *crypto.Cipher

	log *slog.Logger
}

// NewFortnoxConnector creates a connector. configs maps each supported mode to its
// Fortnox OAuth credentials. modes without an entry in configs will show an error.
func NewFortnoxConnector(
	configs map[config.Mode]config.Fortnox,
	tokenStore domain.TokenStore,
	tenantRepo domain.TenantRepository,
	sessions *auth.SessionManager,
	integrations domain.IntegrationStore,
	cipher *crypto.Cipher,
	log *slog.Logger,
) *FortnoxConnector {
	return &FortnoxConnector{
		configs:      configs,
		tokenStore:   tokenStore,
		tenantRepo:   tenantRepo,
		sessions:     sessions,
		integrations: integrations,
		cipher:       cipher,
		log:          log,
	}
}

// resolveFor returns the Fortnox credentials this owner should use: their own
// registered integration if they have one, otherwise the application-level
// credentials for that mode (ADR-0005).
//
// Every place that builds an authorize URL or exchanges a code must go through
// here. Authorizing with the owner's client id and exchanging with ours fails
// at Fortnox with an error naming neither.
func (c *FortnoxConnector) resolveFor(r *http.Request, sess *auth.Session, mode config.Mode) (config.Fortnox, error) {
	appLevel, ok := c.configs[mode]
	if !ok {
		return config.Fortnox{}, fmt.Errorf("no Fortnox config for mode %s", mode)
	}
	return integration.Resolve(r.Context(), c.integrations, c.cipher, sess.Owner, appLevel)
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

	statuses := c.modeStatuses(r.Context(), sess)

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
	// A company is required. Before this, disconnect deleted "<owner>:<mode>" —
	// a key no token has ever been stored under — so it reported success and
	// removed nothing.
	company := auth.CompanyKey(r.FormValue("company"))
	if company == "" {
		company = auth.CompanyKey(sess.Company)
	}
	if company == "" {
		http.Error(w, "no company given: disconnect needs to know which company to disconnect", http.StatusBadRequest)
		return
	}

	tenantID := auth.TenantKey(sess.Owner, mode, company)
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
	cfg, err := c.resolveFor(r, sess, mode)
	if err != nil {
		// Fail closed. Falling back to the application-level credentials here
		// would send the owner to authorize the OPERATOR's integration, which
		// looks like it worked.
		c.log.Error("resolve fortnox integration", "owner", sess.Owner, "mode", mode, "err", err)
		http.Error(w, "could not determine which Fortnox integration to use", http.StatusInternalServerError)
		return
	}

	// Bind this authorization to this session. `state` used to be the mode
	// alone, which meant it carried routing information and provided no CSRF
	// protection at all (#83).
	nonce, err := auth.NewOAuthNonce()
	if err != nil {
		c.log.Error("generate oauth nonce", "err", err)
		http.Error(w, "could not start authorization", http.StatusInternalServerError)
		return
	}
	if c.sessions == nil {
		http.Error(w, "session manager unavailable", http.StatusInternalServerError)
		return
	}
	pending := *sess
	pending.FortnoxNonce = nonce
	pending.FortnoxNonceAt = time.Now()
	if err := c.sessions.Set(w, pending); err != nil {
		c.log.Error("persist oauth nonce", "err", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	// Fortnox OAuth2 authorization endpoint.
	params := url.Values{
		"client_id":     {cfg.ClientID},
		"redirect_uri":  {cfg.RedirectURI},
		"scope":         {cfg.Scopes},
		"response_type": {"code"},
		"state":         {auth.OAuthState(nonce, string(mode))},
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
	presentedNonce, modeFromState, ok := auth.ParseOAuthState(r.URL.Query().Get("state"))
	if !ok {
		// Includes the old mode-only format. Accepting that "for
		// compatibility" would leave the hole open.
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	if !sess.OAuthNonceValid(presentedNonce, time.Now()) {
		// A callback carrying a nonce this session did not issue, or one that
		// has expired. Refusing is the whole point: otherwise anyone able to
		// make a logged-in browser issue this request could bind a Fortnox
		// authorization of their choosing to this session (#83).
		c.log.Error("fortnox callback refused: state nonce does not match this session", "owner", sess.Owner)
		http.Error(w, "this authorization did not start here — please try connecting again", http.StatusBadRequest)
		return
	}
	mode := config.Mode(modeFromState)
	if !mode.IsValid() {
		http.Error(w, "invalid mode in state", http.StatusBadRequest)
		return
	}
	cfg, err := c.resolveFor(r, sess, mode)
	if err != nil {
		c.log.Error("resolve fortnox integration", "owner", sess.Owner, "mode", mode, "err", err)
		http.Error(w, "could not determine which Fortnox integration to use", http.StatusInternalServerError)
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

	tenantID := auth.TenantKey(sess.Owner, mode, companyKey)

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

	// Consume the nonce so a replayed callback fails, and make the
	// just-connected company the active one — connecting it is what starting
	// to work with it means.
	updated := *sess
	updated.FortnoxNonce = ""
	updated.FortnoxNonceAt = time.Time{}
	updated.Company = companyKey
	if err := c.sessions.Set(w, updated); err != nil {
		c.log.Error("persist session after connect", "tenant", tenantID, "err", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	c.log.Info("fortnox connected", "tenant", tenantID, "mode", mode, "company", company.Name)
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
	for _, st := range c.modeStatuses(r.Context(), sess) {
		parts = append(parts, fmt.Sprintf(`%q:%v`, st.Mode, st.Connected))
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
	prefix := sess.Owner + ":" + string(sess.Mode) + ":"
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
		out = append(out, CompanyChoice{Name: t.Name, OrgNumber: key, Active: key == active, Mode: sess.Mode})
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

	tenantID := auth.TenantKey(sess.Owner, sess.Mode, company)
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

// modeStatuses reports, per configured mode, whether the owner has any company
// connected in it.
//
// "Connected" is now a property of (mode, company), so a mode is connected if
// ANY of its companies is. The previous implementation probed a single
// "<owner>:<mode>" key — the pre-company format — which no stored token
// matches, so every mode reported disconnected regardless of reality.
func (c *FortnoxConnector) modeStatuses(ctx context.Context, sess *auth.Session) []ModeStatus {
	orderedModes := []config.Mode{config.ModeSandbox, config.ModeProduction}
	statuses := make([]ModeStatus, 0, len(orderedModes))

	for _, mode := range orderedModes {
		if _, ok := c.configs[mode]; !ok {
			continue
		}
		statuses = append(statuses, ModeStatus{Mode: mode, Connected: c.anyCompanyConnected(ctx, sess.Owner, mode)})
	}
	return statuses
}

// anyCompanyConnected reports whether a token exists for any company this
// owner has registered in this mode.
func (c *FortnoxConnector) anyCompanyConnected(ctx context.Context, owner string, mode config.Mode) bool {
	if c.tenantRepo == nil {
		return false
	}
	prefix := owner + ":" + string(mode) + ":"
	tenants, err := c.tenantRepo.ListByPrefix(ctx, prefix)
	if err != nil {
		// Report disconnected rather than guessing connected: an unreadable
		// registry must not render a Connect button as though it were done.
		c.log.Error("list companies for mode status", "prefix", prefix, "err", err)
		return false
	}
	for _, t := range tenants {
		if _, err := c.tokenStore.Load(ctx, t.ID); err == nil {
			return true
		}
	}
	return false
}
