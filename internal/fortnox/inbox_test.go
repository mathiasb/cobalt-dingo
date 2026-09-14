package fortnox

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Shapes taken from the vendored OpenAPI spec (docs/vendor/fortnox-openapi.json,
// GET /3/inbox), not from a guess: the HTML documentation does not describe
// these fields at all, which is why the spec was vendored.
const inboxBody = `{"Folder":{
  "@url":"https://api.fortnox.se/3/inbox/root",
  "Email":"inbox.lev.5568360688@arkivplats.se",
  "Id":"root","Name":"Inkorg",
  "Files":[
    {"@url":"https://api.fortnox.se/3/inbox/f1","ArchiveFileId":"af-1","Comments":"","Id":"f1","Name":"faktura.pdf","Path":"/","Size":20481}
  ],
  "Folders":[
    {"@url":"https://api.fortnox.se/3/inbox/sub","Email":"inbox.ver.5568360688@arkivplats.se","Id":"sub","Name":"Verifikat","Files":[],"Folders":[]}
  ]}}`

func TestGetInbox_readsFilesAndTheArchiveAddress(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(inboxBody))
	}))
	defer srv.Close()

	folder, err := NewClient(srv.URL, "tok", true).GetInbox()
	require.NoError(t, err)

	assert.Equal(t, "/3/inbox", gotPath)
	assert.Equal(t, http.MethodGet, gotMethod)

	// Folder.Email is the authoritative arkivplats address. The repo currently
	// hardcodes these, which is how a stale inbox.lev address kept accepting
	// mail that never arrived (#80).
	assert.Equal(t, "inbox.lev.5568360688@arkivplats.se", folder.Email)
	assert.Equal(t, "Inkorg", folder.Name)

	require.Len(t, folder.Files, 1)
	assert.Equal(t, "faktura.pdf", folder.Files[0].Name)
	assert.Equal(t, "af-1", folder.Files[0].ArchiveFileID,
		"ArchiveFileId is the join to /3/supplierinvoicefileconnections — without it there is no way to ask whether a file was booked")
	assert.Equal(t, 20481, folder.Files[0].Size)

	require.Len(t, folder.Folders, 1)
	assert.Equal(t, "inbox.ver.5568360688@arkivplats.se", folder.Folders[0].Email,
		"subfolders carry their own address: ver and lev are different inboxes")
}

// The whole point of reading the inbox is that the scope was granted. A 403 is
// a missing `inbox` scope, and it must say so rather than surfacing as a bare
// status code — the remedy is a re-authorization, which is expensive and
// non-obvious.
func TestGetInbox_namesTheMissingScopeOn403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"ErrorInformation":{"Code":2000594,"Message":"Access denied"}}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok", true).GetInbox()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInboxScopeMissing))
	assert.Contains(t, err.Error(), "inbox")
}

// Reading the inbox must never be able to write to it: the same endpoint takes
// POST to upload and DELETE to remove.
func TestGetInbox_isRefusedByTheReadOnlyGateOnWrite(t *testing.T) {
	c := NewClient("http://example.invalid", "tok", true)
	_, err := c.do(mustRequest(t, http.MethodDelete, "http://example.invalid/3/inbox/f1"))
	assert.True(t, errors.Is(err, ErrReadOnlyClient))
}

func mustRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	return req
}
