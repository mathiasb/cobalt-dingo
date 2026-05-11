// Package auth – OAuth2 Authorization Code Flow for Fortnox.
//
// Usage (one-time setup):
//
//	cfg := auth.OAuthConfig{ ... }
//	authURL, state := auth.AuthorizationURL(cfg)
//	// open authURL in browser
//	srv, _ := auth.NewCallbackServer(":8080")
//	result := srv.Wait(ctx)
//	auth.ValidateState(state, result.State)
//	token, _ := auth.ExchangeCode(ctx, cfg, result.Code)
//	store.Save(token)
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	fortnoxAuthEndpoint  = "https://apps.fortnox.se/oauth-v1/auth"
	fortnoxTokenEndpoint = "https://apps.fortnox.se/oauth-v1/token"
)

// OAuthConfig holds the parameters for the Fortnox OAuth2 flow.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
	// AuthEndpoint and TokenEndpoint can be overridden for tests.
	AuthEndpoint  string
	TokenEndpoint string
}

func (c OAuthConfig) authEndpoint() string {
	if c.AuthEndpoint != "" {
		return c.AuthEndpoint
	}
	return fortnoxAuthEndpoint
}

func (c OAuthConfig) tokenEndpoint() string {
	if c.TokenEndpoint != "" {
		return c.TokenEndpoint
	}
	return fortnoxTokenEndpoint
}

// AuthorizationURL builds the Fortnox authorization URL and returns it together
// with a random state token for CSRF protection. The caller must verify that the
// state returned in the callback matches the one returned here.
func AuthorizationURL(cfg OAuthConfig) (authURL, state string) {
	state = randomState()
	scopes := strings.Join(cfg.Scopes, " ")

	q := url.Values{
		"client_id":     {cfg.ClientID},
		"redirect_uri":  {cfg.RedirectURI},
		"scope":         {scopes},
		"response_type": {"code"},
		"state":         {state},
		"access_type":   {"offline"}, // request refresh token
	}
	return cfg.authEndpoint() + "?" + q.Encode(), state
}

// ExchangeCode exchanges an authorization code for an access+refresh token pair.
func ExchangeCode(ctx context.Context, cfg OAuthConfig, code string) (*Token, error) {
	body := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"redirect_uri":  {cfg.RedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.tokenEndpoint(),
		strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth: token endpoint returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("oauth: parse token response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("oauth: %s", result.Error)
	}
	return &Token{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		TokenType:    result.TokenType,
		Expiry:       time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}, nil
}

// ValidateState returns an error if got does not match expected, or if either is empty.
// Call this before exchanging the authorization code to prevent CSRF attacks.
func ValidateState(expected, got string) error {
	if expected == "" || got == "" {
		return errors.New("oauth: state must not be empty")
	}
	if expected != got {
		return fmt.Errorf("oauth: state mismatch – possible CSRF attack (expected %q, got %q)", expected, got)
	}
	return nil
}

// CallbackResult holds the result captured from the OAuth2 redirect.
type CallbackResult struct {
	Code  string
	State string
	Err   error
}

// CallbackServer listens for the OAuth2 redirect on localhost.
type CallbackServer struct {
	listener net.Listener
	server   *http.Server
	resultCh chan CallbackResult
}

// NewCallbackServer creates a server listening on addr (e.g. ":8080" or ":0" for random port).
func NewCallbackServer(addr string) (*CallbackServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("oauth: listen on %s: %w", addr, err)
	}
	s := &CallbackServer{
		listener: ln,
		resultCh: make(chan CallbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)
	s.server = &http.Server{Handler: mux}
	go s.server.Serve(ln) //nolint:errcheck
	return s, nil
}

// Port returns the port the server is actually listening on.
// Useful when ":0" was used to get a random port.
func (s *CallbackServer) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// Wait blocks until the callback is received or ctx is cancelled.
func (s *CallbackServer) Wait(ctx context.Context) CallbackResult {
	select {
	case result := <-s.resultCh:
		_ = s.server.Shutdown(context.Background())
		return result
	case <-ctx.Done():
		_ = s.server.Shutdown(context.Background())
		return CallbackResult{Err: fmt.Errorf("oauth: timed out waiting for callback: %w", ctx.Err())}
	}
}

func (s *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if errParam := q.Get("error"); errParam != "" {
		desc := q.Get("error_description")
		if desc == "" {
			desc = errParam
		}
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "<html><body><h2>Autentisering misslyckades</h2><p>%s</p></body></html>", desc) //nolint:errcheck
		s.resultCh <- CallbackResult{Err: fmt.Errorf("oauth: authorization denied: %s", errParam)}
		return
	}

	code := q.Get("code")
	if code == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "<html><body><h2>Saknad kod</h2></body></html>") //nolint:errcheck
		s.resultCh <- CallbackResult{Err: errors.New("oauth: callback missing code parameter")}
		return
	}

	_, _ = fmt.Fprint(w, `<html><body style="font-family:sans-serif;text-align:center;padding:4rem">
<h2>✓ Autentisering lyckades</h2>
<p>Du kan stänga det här fönstret och gå tillbaka till terminalen.</p>
</body></html>`)
	s.resultCh <- CallbackResult{Code: code, State: q.Get("state")}
}

// randomState generates a cryptographically random state string.
func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
