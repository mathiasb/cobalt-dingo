// Package auth provides OIDC login, session management, and auth middleware.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/config"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

const cookieName = "cd_session"

type contextKey string

const sessionKey contextKey = "session"

// Session holds the authenticated user's identity, active Fortnox mode and
// active company.
type Session struct {
	// Owner is the internal user ID that credentials are keyed by (ADR-0003).
	// Minted at first login and resolved thereafter from the verified email, so
	// an identity-provider change is a login concern and not a
	// credential-affecting event.
	Owner string `json:"owner"`

	// Sub is the OIDC subject, kept for audit and debugging only. It is
	// provider-scoped and opaque: Authentik's sub_mode alone changes its shape,
	// and recreating a provider can change it with no migration involved. It
	// MUST NOT be used as a credential lookup key — that orphaned a live
	// Fortnox token once already, silently.
	Sub string `json:"sub"`

	Email string      `json:"email"`
	Name  string      `json:"name"`
	Mode  config.Mode `json:"mode"`

	// Company is the normalised organisation number of the company being
	// worked with — see CompanyKey. Empty means none selected, which is an
	// error at TenantID() rather than a default.
	Company string `json:"company,omitempty"`

	ExpiresAt time.Time `json:"exp"`
}

// ErrNoCompanySelected is returned when a session has no active company.
//
// It is deliberately an error rather than a fallback. The previous key was
// "<sub>:<mode>", which gave one connection per mode — so connecting a second
// company overwrote the first, and any code that guessed a default would be
// reading and writing whichever company happened to occupy that row.
var ErrNoCompanySelected = errors.New("no company selected: pick one at /fortnox/ before touching company data")

// ErrEmailNotVerified is returned when the identity provider does not assert
// that the email address is verified.
//
// ADR-0003 rests on the email claim being verified: keying credentials on an
// unverified email is WORSE than keying on the subject, because it is
// attacker-influenceable rather than merely unstable. So this refuses a session
// rather than degrading to an unverified key.
var ErrEmailNotVerified = errors.New("identity provider did not assert the email address is verified")

// OwnerDirectory maps a verified email to the internal user ID credentials are
// keyed by, minting one on first sight.
//
// It takes the subject too, so it can be recorded against the user for audit —
// which is the only thing the subject is for now.
type OwnerDirectory interface {
	ResolveOwner(ctx context.Context, email, sub string) (string, error)
}

// NewSession builds a session from a verified identity, resolving the
// credential owner from the verified email.
//
// It refuses rather than producing a session with an empty or unverified
// identity: every one of those would yield a credential key that either
// collides with another user's or names nobody.
func NewSession(ctx context.Context, dir OwnerDirectory, ident verifiedIdentity, mode config.Mode) (Session, error) {
	email := strings.TrimSpace(strings.ToLower(ident.Email))
	if email == "" {
		return Session{}, errors.New("identity provider returned no email address: cannot resolve a credential owner")
	}
	if !ident.EmailVerified {
		return Session{}, fmt.Errorf("%w: %s", ErrEmailNotVerified, email)
	}
	if dir == nil {
		return Session{}, errors.New("no owner directory configured: cannot resolve a credential owner")
	}

	owner, err := dir.ResolveOwner(ctx, email, ident.Sub)
	if err != nil {
		return Session{}, fmt.Errorf("resolve credential owner: %w", err)
	}
	if strings.TrimSpace(owner) == "" {
		return Session{}, errors.New("owner directory returned an empty id")
	}

	return Session{
		Owner: owner,
		Sub:   ident.Sub,
		Email: email,
		Name:  ident.Name,
		Mode:  mode,
	}, nil
}

// TenantID returns the composite tenant key used for Fortnox token storage.
//
// Format: "<owner>:<mode>:<company>". All three parts are required: the same
// company in sandbox and in production are separate connections, and two
// companies must never share a token row.
//
// The owner is the internal user ID, NOT the OIDC subject (ADR-0003). A
// subject-derived key orphaned a live Fortnox token during the Dex→Authentik
// migration — the credential was not deleted, it sat under a key nobody would
// ever compute again. scripts/assert-credential-key-stability.sh asserts on
// this function's body for exactly that reason.
func (s Session) TenantID() (domain.TenantID, error) {
	owner := strings.TrimSpace(s.Owner)
	if owner == "" {
		return "", errors.New("session has no credential owner: cannot derive a tenant key")
	}
	if !s.Mode.IsValid() {
		return "", fmt.Errorf("session has no valid Fortnox mode (got %q)", s.Mode)
	}
	company := CompanyKey(s.Company)
	if company == "" {
		return "", ErrNoCompanySelected
	}
	return TenantKey(owner, s.Mode, company), nil
}

// TenantKey builds the credential key. THE single derivation — every caller
// must use it.
//
// It exists because the key was being rebuilt by hand in nine places, and
// three of them still produced the pre-company two-part form after the company
// was added. The result was a status page that always said "Not connected" and
// a disconnect button that deleted nothing, because both probed a key no stored
// token could ever have. A key with more than one construction site has more
// than one format.
func TenantKey(owner string, mode config.Mode, company string) domain.TenantID {
	return domain.TenantID(owner + ":" + string(mode) + ":" + CompanyKey(company))
}

// CompanyKey normalises a Fortnox organisation number into a stable key.
//
// Fortnox returns it formatted ("556677-8899"). Keeping only the digits means a
// reconnect cannot create a second row for the same company because the
// formatting differed — and an input with no digits at all yields "", which is
// what lets TenantID fail closed.
func CompanyKey(orgNumber string) string {
	var b strings.Builder
	for _, r := range orgNumber {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SessionManager encodes and decodes signed session cookies.
type SessionManager struct {
	secret []byte
}

// NewSessionManager creates a SessionManager using the given HMAC secret.
func NewSessionManager(secret string) *SessionManager {
	return &SessionManager{secret: []byte(secret)}
}

// Set writes a signed session cookie to w.
func (m *SessionManager) Set(w http.ResponseWriter, s Session) error {
	s.ExpiresAt = time.Now().Add(24 * time.Hour)
	payload, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	enc := base64.RawURLEncoding.EncodeToString(payload)
	sig := m.sign(enc)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    enc + "." + sig,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  s.ExpiresAt,
	})
	return nil
}

// Get reads and verifies the session cookie from r.
// Returns nil if no cookie, invalid signature, or expired.
func (m *SessionManager) Get(r *http.Request) *Session {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	enc, sig := parts[0], parts[1]
	if !hmac.Equal([]byte(m.sign(enc)), []byte(sig)) {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil
	}
	var s Session
	if err := json.Unmarshal(payload, &s); err != nil {
		return nil
	}
	if time.Now().After(s.ExpiresAt) {
		return nil
	}
	return &s
}

// Clear deletes the session cookie.
func (m *SessionManager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (m *SessionManager) sign(data string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// FromContext retrieves the session injected by the auth middleware.
func FromContext(r *http.Request) *Session {
	s, _ := r.Context().Value(sessionKey).(*Session)
	return s
}

// WithSession injects s into ctx. Use in tests only.
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionKey, s)
}
