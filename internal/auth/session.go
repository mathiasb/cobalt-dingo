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
	Sub   string      `json:"sub"`
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

// TenantID returns the composite tenant key used for Fortnox token storage.
//
// Format: "<sub>:<mode>:<company>", e.g. "mathias-local:production:5566778899".
// All three parts are required: the same company in sandbox and in production
// are separate connections, and two companies must never share a token row.
func (s Session) TenantID() (domain.TenantID, error) {
	if strings.TrimSpace(s.Sub) == "" {
		return "", errors.New("session has no subject: cannot derive a tenant key")
	}
	if !s.Mode.IsValid() {
		return "", fmt.Errorf("session has no valid Fortnox mode (got %q)", s.Mode)
	}
	company := CompanyKey(s.Company)
	if company == "" {
		return "", ErrNoCompanySelected
	}
	return domain.TenantID(s.Sub + ":" + string(s.Mode) + ":" + company), nil
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
