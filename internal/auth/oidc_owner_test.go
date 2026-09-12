package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownerHandler builds a handler with a directory wired in, reusing the
// existing harness so this exercises the real CallbackHandler.
func ownerHandler(t *testing.T, dir OwnerDirectory, ident verifiedIdentity) *OIDCHandler {
	t.Helper()
	h := newTestHandler(t, fakeVerifier{ident: ident})
	h.directory = dir
	return h
}

// A login must produce a session whose credential key is the internal owner
// id. Without the directory wired in, login would succeed and every credential
// lookup afterwards would fail — the failure arriving nowhere near its cause.
func TestCallbackHandler_SetsTheCredentialOwner(t *testing.T) {
	dir := newFakeDirectory()
	h := ownerHandler(t, dir, verifiedIdentity{
		Nonce: "n", Sub: "authentik-sub", Email: "Mathias@D-MA.be", EmailVerified: true,
	})

	w := httptest.NewRecorder()
	h.CallbackHandler(w, callbackRequest("state-1", "n"))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusFound, res.StatusCode, "body: %s", w.Body.String())
	require.NotEmpty(t, sessionCookie(res), "a session must be issued")
	assert.Equal(t, []string{"authentik-sub"}, dir.seen, "the subject is recorded, for audit only")
	assert.Contains(t, dir.byEmail, "mathias@d-ma.be", "the email is lowercased before resolution")
}

// ADR-0003's premise, enforced at the door: keying credentials on an
// unverified email is worse than keying on the subject, because it is
// attacker-influenceable rather than merely unstable.
func TestCallbackHandler_RefusesAnUnverifiedEmail(t *testing.T) {
	dir := newFakeDirectory()
	h := ownerHandler(t, dir, verifiedIdentity{
		Nonce: "n", Sub: "authentik-sub", Email: "mathias@d-ma.be", EmailVerified: false,
	})

	w := httptest.NewRecorder()
	h.CallbackHandler(w, callbackRequest("state-1", "n"))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusForbidden, res.StatusCode)
	assert.Empty(t, sessionCookie(res), "no session may be issued")
	assert.Empty(t, dir.seen, "and no user may be minted")
}

// No directory configured must fail the login rather than issue a session with
// an empty credential owner.
func TestCallbackHandler_RefusesWithNoDirectory(t *testing.T) {
	h := ownerHandler(t, nil, verifiedIdentity{
		Nonce: "n", Sub: "s", Email: "mathias@d-ma.be", EmailVerified: true,
	})

	w := httptest.NewRecorder()
	h.CallbackHandler(w, callbackRequest("state-1", "n"))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Empty(t, sessionCookie(res))
}
