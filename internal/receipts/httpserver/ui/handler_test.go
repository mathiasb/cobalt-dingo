package ui_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver/ui"
	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver/ui/stub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMux(tokens *stub.Tokens, keys *stub.Keys, audit *stub.Audit, health *stub.Health) *http.ServeMux {
	mux := http.NewServeMux()
	h := ui.NewHandler(tokens, keys, audit, health)
	h.Register(mux, "/ui")
	return mux
}

func get(mux http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestStatus_Connected(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())
	rec := get(mux, "/ui/")
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Ansluten")
	assert.Contains(t, body, "Nåbar")
}

func TestStatus_Disconnected(t *testing.T) {
	mux := newMux(stub.DisconnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())
	rec := get(mux, "/ui/")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Saknas")
}

func TestAuth_ShowsConnectButton_WhenDisconnected(t *testing.T) {
	mux := newMux(stub.DisconnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())
	rec := get(mux, "/ui/auth")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Anslut till Fortnox")
}

func TestAuth_ShowsRefreshButton_WhenConnected(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())
	rec := get(mux, "/ui/auth")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Förnya token nu")
	assert.Contains(t, rec.Body.String(), "Koppla från")
}

func TestTokenRefresh_UpdatesExpiry(t *testing.T) {
	tokens := stub.ConnectedTokens()
	mux := newMux(tokens, stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ui/token/refresh", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Förnya token nu")
}

func TestTokenRevoke_ShowsConnectButton(t *testing.T) {
	tokens := stub.ConnectedTokens()
	mux := newMux(tokens, stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/ui/token", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Anslut till Fortnox")
}

func TestKeys_ListsSeededKeys(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(3), stub.SeedAudit(0), stub.AllHealthy())
	rec := get(mux, "/ui/keys")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Exempelnyckel 1")
	assert.Contains(t, body, "Exempelnyckel 3")
}

func TestKeys_EmptyState(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())
	rec := get(mux, "/ui/keys")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Inga nycklar skapade")
}

func TestKeyForm_RendersForm(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())
	rec := get(mux, "/ui/keys/new")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `name="label"`)
}

func TestKeyCreate_ReturnsRow(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())

	form := url.Values{"label": {"Testnyckel"}}
	req := httptest.NewRequest(http.MethodPost, "/ui/keys",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Testnyckel")
}

func TestKeyCreate_RejectsMissingLabel(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())

	req := httptest.NewRequest(http.MethodPost, "/ui/keys",
		strings.NewReader("label="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestKeyRevoke_Returns200(t *testing.T) {
	keys := stub.SeedKeys(1)
	mux := newMux(stub.ConnectedTokens(), keys, stub.SeedAudit(0), stub.AllHealthy())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/ui/keys/stub-1", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuditLog_ShowsEvents(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(5), stub.AllHealthy())
	rec := get(mux, "/ui/audit")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "/invoices")
}

func TestAuditLog_EmptyState(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(0), stub.AllHealthy())
	rec := get(mux, "/ui/audit")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Inga händelser")
}

func TestAuditLog_PageQueryParam(t *testing.T) {
	mux := newMux(stub.ConnectedTokens(), stub.SeedKeys(0), stub.SeedAudit(10), stub.AllHealthy())
	rec := get(mux, "/ui/audit?page=2")
	assert.Equal(t, http.StatusOK, rec.Code)
}
