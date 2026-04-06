package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mathiasb/coo-agent/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AuthorizationURL ---

func TestOAuth_AuthorizationURL_ContainsRequiredParams(t *testing.T) {
	cfg := auth.OAuthConfig{
		ClientID:    "test-client-id",
		RedirectURI: "http://localhost:8080/callback",
		Scopes:      []string{"bookkeeping", "invoice"},
	}
	rawURL, state := auth.AuthorizationURL(cfg)

	u, err := url.Parse(rawURL)
	require.NoError(t, err)

	q := u.Query()
	assert.Equal(t, "test-client-id", q.Get("client_id"))
	assert.Equal(t, "http://localhost:8080/callback", q.Get("redirect_uri"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Contains(t, q.Get("scope"), "bookkeeping")
	assert.Contains(t, q.Get("scope"), "invoice")
	assert.NotEmpty(t, q.Get("state"), "state parameter must be set for CSRF protection")
	assert.Equal(t, state, q.Get("state"), "returned state must match URL state parameter")
}

func TestOAuth_AuthorizationURL_GeneratesUniqueStateEachCall(t *testing.T) {
	cfg := auth.OAuthConfig{ClientID: "id", RedirectURI: "http://localhost:8080/callback"}
	_, state1 := auth.AuthorizationURL(cfg)
	_, state2 := auth.AuthorizationURL(cfg)
	assert.NotEqual(t, state1, state2, "each call must produce a unique state to prevent CSRF replay")
}

// --- ExchangeCode ---

func TestOAuth_ExchangeCode_SendsCorrectRequest(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotBody = string(body)
		writeTokenResponse(w, "acc-123", "ref-456", 3600)
	}))
	defer srv.Close()

	cfg := auth.OAuthConfig{
		ClientID:      "my-client",
		ClientSecret:  "my-secret",
		RedirectURI:   "http://localhost:8080/callback",
		TokenEndpoint: srv.URL,
	}
	token, err := auth.ExchangeCode(context.Background(), cfg, "auth-code-xyz")
	require.NoError(t, err)

	assert.Contains(t, gotBody, "grant_type=authorization_code")
	assert.Contains(t, gotBody, "code=auth-code-xyz")
	assert.Contains(t, gotBody, "client_id=my-client")
	assert.Contains(t, gotBody, "client_secret=my-secret")

	assert.Equal(t, "acc-123", token.AccessToken)
	assert.Equal(t, "ref-456", token.RefreshToken)
	assert.False(t, token.IsExpired())
}

func TestOAuth_ExchangeCode_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid_client"}`)
	}))
	defer srv.Close()

	cfg := auth.OAuthConfig{
		ClientID:      "id",
		ClientSecret:  "secret",
		RedirectURI:   "http://localhost:8080/callback",
		TokenEndpoint: srv.URL,
	}
	_, err := auth.ExchangeCode(context.Background(), cfg, "bad-code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestOAuth_ExchangeCode_TokenExpirySetFromExpiresIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTokenResponse(w, "acc", "ref", 7200)
	}))
	defer srv.Close()

	cfg := auth.OAuthConfig{ClientID: "id", ClientSecret: "s", RedirectURI: "r", TokenEndpoint: srv.URL}
	before := time.Now().Add(7199 * time.Second)

	token, err := auth.ExchangeCode(context.Background(), cfg, "code")
	require.NoError(t, err)
	assert.True(t, token.Expiry.After(before), "expiry should be ~7200s from now")
}

// --- CallbackServer ---

func TestOAuth_CallbackServer_CapturesCodeAndState(t *testing.T) {
	srv, err := auth.NewCallbackServer(":0") // random port
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resultCh := make(chan auth.CallbackResult, 1)
	go func() {
		resultCh <- srv.Wait(ctx)
	}()

	// Simulate Fortnox redirect
	port := srv.Port()
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/callback?code=the-auth-code&state=the-state", port))
	require.NoError(t, err)
	resp.Body.Close()

	result := <-resultCh
	assert.NoError(t, result.Err)
	assert.Equal(t, "the-auth-code", result.Code)
	assert.Equal(t, "the-state", result.State)
}

func TestOAuth_CallbackServer_ReturnsErrorOnMissingCode(t *testing.T) {
	srv, err := auth.NewCallbackServer(":0")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resultCh := make(chan auth.CallbackResult, 1)
	go func() {
		resultCh <- srv.Wait(ctx)
	}()

	port := srv.Port()
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/callback?error=access_denied", port))
	require.NoError(t, err)
	resp.Body.Close()

	result := <-resultCh
	assert.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "access_denied")
}

func TestOAuth_CallbackServer_ShowsSuccessPageToUser(t *testing.T) {
	srv, err := auth.NewCallbackServer(":0")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { srv.Wait(ctx) }() //nolint:errcheck

	port := srv.Port()
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/callback?code=x&state=y", port))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body := make([]byte, 512)
	n, _ := resp.Body.Read(body)
	assert.True(t, strings.Contains(string(body[:n]), "✓") || strings.Contains(string(body[:n]), "lyckades") || strings.Contains(string(body[:n]), "OK"),
		"success page should confirm to the user that auth worked")
}

// --- State validation ---

func TestOAuth_ValidateState_MatchingStateIsValid(t *testing.T) {
	assert.NoError(t, auth.ValidateState("abc123", "abc123"))
}

func TestOAuth_ValidateState_MismatchReturnsError(t *testing.T) {
	err := auth.ValidateState("expected", "got-something-else")
	assert.Error(t, err, "mismatched state must be rejected to prevent CSRF")
}

func TestOAuth_ValidateState_EmptyStateReturnsError(t *testing.T) {
	assert.Error(t, auth.ValidateState("expected", ""))
	assert.Error(t, auth.ValidateState("", ""))
}

// --- helpers ---

func writeTokenResponse(w http.ResponseWriter, access, refresh string, expiresIn int) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"access_token":%q,"refresh_token":%q,"expires_in":%d,"token_type":"Bearer"}`,
		access, refresh, expiresIn)
}
