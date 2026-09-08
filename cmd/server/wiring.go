package main

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// errNoAuthenticator is returned when the server would otherwise serve the
// application without any authentication in front of it.
var errNoAuthenticator = errors.New(
	"REFUSING TO SERVE: no authenticator — every route would be public")

// publicPrefixes are the paths served without a session: the liveness probe,
// static assets, and the login flow itself (which cannot require a session).
var publicPrefixes = []string{"/healthz", "/static/", "/auth/"}

// secureHandler wraps mux with authentication, or refuses.
//
// It exists as a function rather than a block in main() because that is the
// only way it can be tested, and this is not a hypothetical concern: the
// fail-open it replaces lived undetected inside main() precisely because
// nothing there is reachable from a test.
//
// The refusal is deliberate and has no config default that turns it off in
// production. An unauthenticated server starts perfectly well and answers every
// request with a 200, so nothing downstream — not the liveness probe, not the
// ingress, not a human looking at the pod — will report the difference.
// Crashing is the only failure mode that gets noticed.
//
// allowUnauthenticated is the local-development escape hatch. It applies ONLY
// when no IdP was configured at all. An IdP that was asked for and could not be
// built is a fault, not a choice, and the opt-in must not launder it: that is
// the exact production case (a co-boot race against Authentik) this guards.
func secureHandler(
	mux http.Handler,
	sessions *auth.SessionManager,
	oidcCfg config.OIDC,
	oidcHandler *auth.OIDCHandler,
	allowUnauthenticated bool,
) (http.Handler, error) {
	if oidcHandler != nil {
		return sessions.AuthMiddleware(publicPrefixes, mux), nil
	}
	if oidcCfg.IsEnabled() {
		return nil, fmt.Errorf(
			"%w: OIDC_ISSUER_URL is set (%s) but the OIDC handler could not be built",
			errNoAuthenticator, oidcCfg.IssuerURL)
	}
	if !allowUnauthenticated {
		return nil, fmt.Errorf(
			"%w: set OIDC_ISSUER_URL/OIDC_CLIENT_ID/OIDC_CLIENT_SECRET, or set "+
				"COBALT_ALLOW_UNAUTHENTICATED=true to run without auth (development only)",
			errNoAuthenticator)
	}
	return mux, nil
}
