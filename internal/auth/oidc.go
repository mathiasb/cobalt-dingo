package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// stateCookie and nonceCookie carry the two single-use values that bind a
// callback to the login that started it. state proves this browser began the
// login; nonce binds the returned ID token to this particular authorize
// request. Both are needed — state alone leaves nothing tying the token to the
// attempt that asked for it.
const (
	stateCookie = "oidc_state"
	nonceCookie = "oidc_nonce"
)

// verifiedIdentity is everything the callback needs from a verified ID token.
// The handler works with this rather than *gooidc.IDToken so it does not depend
// on the library's concrete type — and so a test can produce one, which a
// hand-built gooidc.IDToken cannot do (its claims are unexported, so Claims()
// always fails on one).
type verifiedIdentity struct {
	Nonce             string
	Sub               string
	Email             string
	Name              string
	PreferredUsername string
}

// idTokenVerifier is the slice of ID-token verification this package uses. It
// exists so a callback can be exercised without a live identity provider — the
// nonce comparison is otherwise unreachable from a test.
type idTokenVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (verifiedIdentity, error)
}

// gooidcVerifier adapts *gooidc.IDTokenVerifier to idTokenVerifier. Signature
// verification stays entirely in the library; this only reshapes the result.
type gooidcVerifier struct{ v *gooidc.IDTokenVerifier }

func (g gooidcVerifier) Verify(ctx context.Context, rawIDToken string) (verifiedIdentity, error) {
	tok, err := g.v.Verify(ctx, rawIDToken)
	if err != nil {
		return verifiedIdentity{}, fmt.Errorf("verify id_token: %w", err)
	}
	var c struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := tok.Claims(&c); err != nil {
		return verifiedIdentity{}, fmt.Errorf("parse id_token claims: %w", err)
	}
	return verifiedIdentity{
		Nonce:             tok.Nonce,
		Sub:               c.Sub,
		Email:             c.Email,
		Name:              c.Name,
		PreferredUsername: c.PreferredUsername,
	}, nil
}

// OIDCHandler handles the OIDC login/callback/logout routes.
type OIDCHandler struct {
	oauth2cfg   oauth2.Config
	verifier    idTokenVerifier
	sessions    *SessionManager
	defaultMode config.Mode
	log         *slog.Logger
}

// NewOIDCHandler constructs an OIDCHandler, performing OIDC discovery against issuerURL.
func NewOIDCHandler(ctx context.Context, cfg config.OIDC, sessions *SessionManager, defaultMode config.Mode, log *slog.Logger) (*OIDCHandler, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery %s: %w", cfg.IssuerURL, err)
	}
	oauth2cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{gooidc.ScopeOpenID, "email", "profile"},
	}
	verifier := gooidcVerifier{v: provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})}
	return &OIDCHandler{
		oauth2cfg:   oauth2cfg,
		verifier:    verifier,
		sessions:    sessions,
		defaultMode: defaultMode,
		log:         log,
	}, nil
}

// LoginHandler redirects the browser to the OIDC provider.
func (h *OIDCHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce, err := randomState()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setLoginCookie(w, stateCookie, state)
	setLoginCookie(w, nonceCookie, nonce)
	http.Redirect(w, r, h.oauth2cfg.AuthCodeURL(state, gooidc.Nonce(nonce)), http.StatusFound)
}

// setLoginCookie writes one of the short-lived, single-use login cookies.
// The nonce travels the same way state already does rather than in a
// server-side map: this app keeps no session table by design, and a map would
// break any login in flight across a pod restart or a scale-up.
func setLoginCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})
}

// clearLoginCookie expires a login cookie. Both are single-use: leaving either
// in place would let a callback be replayed.
func clearLoginCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", MaxAge: -1})
}

// CallbackHandler exchanges the auth code, verifies the ID token, and sets the session.
func (h *OIDCHandler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	gotState, err := r.Cookie(stateCookie)
	if err != nil || gotState.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	// Read the nonce before anything else consumes the request. A callback
	// without one cannot be validated, so it fails closed rather than skipping
	// the comparison.
	gotNonce, err := r.Cookie(nonceCookie)
	if err != nil || gotNonce.Value == "" {
		http.Error(w, "invalid nonce", http.StatusBadRequest)
		return
	}

	clearLoginCookie(w, stateCookie)
	clearLoginCookie(w, nonceCookie)

	code := r.URL.Query().Get("code")
	tok, err := h.oauth2cfg.Exchange(r.Context(), code)
	if err != nil {
		h.log.Error("oidc token exchange failed", "err", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in response", http.StatusInternalServerError)
		return
	}
	ident, err := h.verifier.Verify(r.Context(), rawID)
	if err != nil {
		h.log.Error("oidc id_token verify failed", "err", err)
		http.Error(w, "token verification failed", http.StatusInternalServerError)
		return
	}

	// Bind the token to this login. An empty nonce is rejected too: a provider
	// that silently dropped the parameter would otherwise disable this check
	// without raising an error anywhere.
	if ident.Nonce == "" || !hmac.Equal([]byte(ident.Nonce), []byte(gotNonce.Value)) {
		h.log.Error("oidc nonce mismatch — id_token does not belong to this login")
		http.Error(w, "invalid nonce", http.StatusBadRequest)
		return
	}

	name := ident.Name
	if name == "" {
		name = ident.PreferredUsername
	}

	s := Session{
		Sub:   ident.Sub,
		Email: ident.Email,
		Name:  name,
		Mode:  h.defaultMode,
	}
	if err := h.sessions.Set(w, s); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	h.log.Info("user logged in", "sub", s.Sub, "email", s.Email)
	http.Redirect(w, r, "/", http.StatusFound)
}

// LogoutHandler clears the session and redirects to home.
func (h *OIDCHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	h.sessions.Clear(w)
	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
