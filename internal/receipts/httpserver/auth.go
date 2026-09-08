package httpserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrNoAuthenticator is returned by New when no authenticator is configured.
// The admin UI mounts token refresh/revoke, API-key issuance and the audit log;
// serving those without credentials is not a degraded mode, it is a breach.
var ErrNoAuthenticator = errors.New(
	"REFUSING TO BUILD: no authenticator — the admin UI would be public")

// Authenticator decides whether a request carries valid admin credentials.
type Authenticator interface {
	Authenticate(r *http.Request) bool
}

// minTokenLength is the shortest admin token accepted. Below this a token is
// guessable and configuring one is indistinguishable from having no auth, which
// is the exact outcome ErrNoAuthenticator exists to prevent.
const minTokenLength = 24

type bearerToken struct{ want [sha256.Size]byte }

// BearerToken builds an Authenticator that accepts `Authorization: Bearer <token>`.
//
// It errors rather than accepting a weak token: a config value that silently
// degrades the control is the same hole with a plausible-looking value in it.
func BearerToken(token string) (Authenticator, error) {
	if len(token) < minTokenLength {
		return nil, fmt.Errorf(
			"admin token must be at least %d characters, got %d", minTokenLength, len(token))
	}
	return bearerToken{want: sha256.Sum256([]byte(token))}, nil
}

// Authenticate compares in constant time. Both sides are hashed first so the
// comparison is over fixed-length input regardless of what the caller sent.
func (b bearerToken) Authenticate(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	sum := sha256.Sum256([]byte(got))
	return hmac.Equal(sum[:], b.want[:])
}

// requireAuth wraps next, rejecting requests the Authenticator does not accept.
func requireAuth(a Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Authenticate(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="cobalt-dingo admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
