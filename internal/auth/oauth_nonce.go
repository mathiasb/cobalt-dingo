package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// oauthNonceTTL bounds how long a Fortnox authorization may take to come back.
//
// Ten minutes is not arbitrary: Fortnox's own authorization code expires in
// ten minutes, so a longer window protects nothing and a shorter one would
// refuse legitimate slow logins. The session itself lasts 24 hours, and a
// nonce living that long would be a 24-hour CSRF window.
const oauthNonceTTL = 10 * time.Minute

// NewOAuthNonce returns a single-use value binding a Fortnox authorization to
// the session that started it.
func NewOAuthNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate oauth nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// OAuthNonceValid reports whether presented matches the nonce this session
// issued, and is still within its window.
//
// Fails closed on every ambiguity: an empty stored nonce never matches (a
// callback that omits the value would otherwise disable the check without
// erroring), an unstamped nonce cannot be aged so it is not trusted, and a
// nonce stamped in the future is refused rather than granted a longer window.
//
// Compared with hmac.Equal rather than ==: the nonce is a secret for the
// duration of the flow, and a length-or-content-dependent comparison is a
// timing side channel.
func (s Session) OAuthNonceValid(presented string, now time.Time) bool {
	stored := strings.TrimSpace(s.FortnoxNonce)
	presented = strings.TrimSpace(presented)
	if stored == "" || presented == "" {
		return false
	}
	if s.FortnoxNonceAt.IsZero() {
		return false
	}
	if s.FortnoxNonceAt.After(now) {
		return false
	}
	if now.Sub(s.FortnoxNonceAt) > oauthNonceTTL {
		return false
	}
	return hmac.Equal([]byte(stored), []byte(presented))
}

// OAuthState builds the `state` parameter: the nonce, plus the mode the
// callback has to route on.
//
// The mode was previously the WHOLE of state, which is why there was no CSRF
// protection at all — state carried routing information and nothing else.
func OAuthState(nonce, mode string) string {
	return nonce + ":" + mode
}

// ParseOAuthState splits a state parameter. Both parts must be present: a
// state with an empty half is malformed, and treating it as partially valid is
// how a check gets skipped.
func ParseOAuthState(state string) (nonce, mode string, ok bool) {
	nonce, mode, found := strings.Cut(state, ":")
	if !found || nonce == "" || mode == "" || strings.Contains(mode, ":") {
		return "", "", false
	}
	return nonce, mode, true
}
