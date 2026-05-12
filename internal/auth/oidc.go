package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// OIDCHandler handles the OIDC login/callback/logout routes.
type OIDCHandler struct {
	oauth2cfg   oauth2.Config
	verifier    *gooidc.IDTokenVerifier
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
	verifier := provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})
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
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})
	http.Redirect(w, r, h.oauth2cfg.AuthCodeURL(state), http.StatusFound)
}

// CallbackHandler exchanges the auth code, verifies the ID token, and sets the session.
func (h *OIDCHandler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oidc_state", MaxAge: -1, Path: "/"})

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
	idToken, err := h.verifier.Verify(r.Context(), rawID)
	if err != nil {
		h.log.Error("oidc id_token verify failed", "err", err)
		http.Error(w, "token verification failed", http.StatusInternalServerError)
		return
	}

	var claims struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "claims parse failed", http.StatusInternalServerError)
		return
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}

	s := Session{
		Sub:   claims.Sub,
		Email: claims.Email,
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
