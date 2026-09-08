package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// fakeVerifier stands in for *gooidc.IDTokenVerifier so a callback can be
// exercised without a live identity provider. It returns whatever ID token the
// test wants, which is the only way to drive the nonce comparison.
type fakeVerifier struct {
	ident verifiedIdentity
	err   error
}

func (f fakeVerifier) Verify(_ context.Context, _ string) (verifiedIdentity, error) {
	return f.ident, f.err
}

// tokenEndpoint returns an httptest server that plays the IdP's token endpoint,
// handing back a syntactically valid OAuth2 response carrying an id_token.
func tokenEndpoint(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at",
			"token_type":   "bearer",
			"id_token":     "irrelevant-the-verifier-is-faked",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestHandler(t *testing.T, v idTokenVerifier) *OIDCHandler {
	t.Helper()
	ts := tokenEndpoint(t)
	return &OIDCHandler{
		oauth2cfg: oauth2.Config{
			ClientID:    "cobalt-dingo",
			RedirectURL: "https://books.d-ma.be/auth/callback",
			Endpoint:    oauth2.Endpoint{AuthURL: "https://auth.d-ma.be/authorize", TokenURL: ts.URL},
		},
		verifier:    v,
		sessions:    NewSessionManager("test-secret"),
		defaultMode: config.ModeSandbox,
		log:         slog.New(slog.DiscardHandler),
	}
}

// The nonce binds the returned ID token to the authorize request that asked for
// it. state alone proves the browser started a login; it does not tie the token
// to this attempt.
func TestLoginHandler_SendsNonceAndStoresIt(t *testing.T) {
	h := newTestHandler(t, fakeVerifier{})

	w := httptest.NewRecorder()
	h.LoginHandler(w, httptest.NewRequest(http.MethodGet, "/auth/login", nil))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusFound, res.StatusCode)

	redirect, err := url.Parse(res.Header.Get("Location"))
	require.NoError(t, err)
	sentNonce := redirect.Query().Get("nonce")
	require.NotEmpty(t, sentNonce, "authorize URL must carry a nonce")

	var stored string
	for _, c := range res.Cookies() {
		if c.Name == nonceCookie {
			stored = c.Value
		}
	}
	require.NotEmpty(t, stored, "the nonce must be stored so the callback can compare it")
	assert.Equal(t, stored, sentNonce, "the stored nonce must be the one that was sent")

	// Same hardening as the state cookie it travels beside.
	for _, c := range res.Cookies() {
		if c.Name != nonceCookie {
			continue
		}
		assert.True(t, c.HttpOnly, "nonce cookie must be HttpOnly")
		assert.True(t, c.Secure, "nonce cookie must be Secure")
		assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	}
}

func TestLoginHandler_NonceDiffersEveryTime(t *testing.T) {
	h := newTestHandler(t, fakeVerifier{})

	seen := map[string]bool{}
	for range 20 {
		w := httptest.NewRecorder()
		h.LoginHandler(w, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
		n := nonceFromRedirect(t, w.Result())
		require.False(t, seen[n], "nonce must not repeat across logins")
		seen[n] = true
	}
}

func nonceFromRedirect(t *testing.T, res *http.Response) string {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	u, err := url.Parse(res.Header.Get("Location"))
	require.NoError(t, err)
	return u.Query().Get("nonce")
}

// callbackRequest builds a callback carrying matching state, plus whatever
// nonce cookie the test wants.
func callbackRequest(state, nonceCookieValue string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+state, nil)
	r.AddCookie(&http.Cookie{Name: "oidc_state", Value: state})
	if nonceCookieValue != "" {
		r.AddCookie(&http.Cookie{Name: nonceCookie, Value: nonceCookieValue})
	}
	return r
}

// This is the half that matters. Sending a nonce and not checking it is the
// same as not having one.
func TestCallbackHandler_RejectsNonceMismatch(t *testing.T) {
	h := newTestHandler(t, fakeVerifier{ident: verifiedIdentity{Nonce: "nonce-from-a-different-login", Sub: "u1"}})

	w := httptest.NewRecorder()
	h.CallbackHandler(w, callbackRequest("state-1", "nonce-we-issued"))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode,
		"an ID token whose nonce does not match this login must be refused")
	assertRejectedByNonceCheck(t, res)
	assert.Empty(t, sessionCookie(res), "no session may be issued on a nonce mismatch")
}

// An ID token with no nonce at all must not pass. A provider that silently
// drops the parameter would otherwise disable the check without any error.
func TestCallbackHandler_RejectsEmptyNonceInToken(t *testing.T) {
	h := newTestHandler(t, fakeVerifier{ident: verifiedIdentity{Nonce: "", Sub: "u1"}})

	w := httptest.NewRecorder()
	h.CallbackHandler(w, callbackRequest("state-1", "nonce-we-issued"))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assertRejectedByNonceCheck(t, res)
	assert.Empty(t, sessionCookie(res))
}

// A callback with no nonce cookie cannot be validated, so it must fail closed
// rather than skip the comparison.
func TestCallbackHandler_RejectsMissingNonceCookie(t *testing.T) {
	h := newTestHandler(t, fakeVerifier{ident: verifiedIdentity{Nonce: "anything", Sub: "u1"}})

	w := httptest.NewRecorder()
	h.CallbackHandler(w, callbackRequest("state-1", ""))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assertRejectedByNonceCheck(t, res)
	assert.Empty(t, sessionCookie(res))
}

func TestCallbackHandler_AcceptsMatchingNonce(t *testing.T) {
	const n = "the-nonce-we-issued"
	h := newTestHandler(t, fakeVerifier{ident: verifiedIdentity{Nonce: n, Sub: "u1", Email: "m@example.com"}})

	w := httptest.NewRecorder()
	h.CallbackHandler(w, callbackRequest("state-1", n))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusFound, res.StatusCode, "a matching nonce must complete the login")
	assert.NotEmpty(t, sessionCookie(res), "a session must be issued")

	// The nonce is single-use: it must be cleared so a replayed callback cannot
	// reuse it.
	var cleared bool
	for _, c := range res.Cookies() {
		if c.Name == nonceCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	assert.True(t, cleared, "the nonce cookie must be cleared after use")
}

// assertRejectedByNonceCheck pins WHICH guard rejected the request. The state
// check also returns 400, so asserting the status alone would keep these tests
// green if the nonce check were deleted and state happened to fail instead.
func assertRejectedByNonceCheck(t *testing.T, res *http.Response) {
	t.Helper()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "invalid nonce",
		"must be rejected by the nonce check, not by an earlier guard")
}

func sessionCookie(res *http.Response) string {
	for _, c := range res.Cookies() {
		if c.Name == cookieName && c.Value != "" {
			return c.Value
		}
	}
	return ""
}
