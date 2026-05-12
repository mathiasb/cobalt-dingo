// Package auth provides OIDC login, session management, and auth middleware.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

const cookieName = "cd_session"

type contextKey string

const sessionKey contextKey = "session"

// Session holds the authenticated user's identity and active Fortnox mode.
type Session struct {
	Sub       string      `json:"sub"`
	Email     string      `json:"email"`
	Name      string      `json:"name"`
	Mode      config.Mode `json:"mode"`
	ExpiresAt time.Time   `json:"exp"`
}

// TenantID returns the composite tenant key used for Fortnox token storage.
// Format: "<sub>:<mode>" (e.g. "mathias-local:sandbox").
func (s Session) TenantID() string {
	return s.Sub + ":" + string(s.Mode)
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
		Name:    cookieName,
		Value:   "",
		Path:    "/",
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
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
