package auth

import (
	"context"
	"net/http"
	"strings"
)

// RequireAuth wraps next and redirects unauthenticated requests to /auth/login.
func (m *SessionManager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := m.Get(r)
		if s == nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, s)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AuthMiddleware wraps next, injecting the session for authenticated requests.
// Paths prefixed by any entry in publicPrefixes bypass the auth check.
// All other unauthenticated requests are redirected to /auth/login.
func (m *SessionManager) AuthMiddleware(publicPrefixes []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, prefix := range publicPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
		}
		s := m.Get(r)
		if s == nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, s)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
