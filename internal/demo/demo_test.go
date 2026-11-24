package demo_test

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/demo"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngBytes is the 8-byte PNG signature; http.DetectContentType sniffs it.
var pngBytes = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 64))

type fakeExtractor struct {
	inv  extract.Invoice
	err  error
	gate chan struct{} // when set, Extract blocks until it is closed
	in   chan struct{} // when set, signalled once Extract has started
}

func (f *fakeExtractor) Extract(ctx context.Context, _ []byte) (extract.Invoice, error) {
	if f.in != nil {
		select {
		case f.in <- struct{}{}:
		default:
		}
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return extract.Invoice{}, ctx.Err()
		}
	}
	return f.inv, f.err
}

func good() extract.Invoice {
	return extract.Invoice{
		Supplier: "Hetzner Online GmbH", InvoiceNumber: "081000972341", InvoiceDate: "2026-11-02",
		Currency: "EUR", NetTotal: 1750, NetKnown: true, VATAmount: 0, Total: 1750,
	}
}

func history() []booking.Example {
	return []booking.Example{
		{Supplier: "Hetzner Online GmbH", Account: 6540, Series: "A", Number: 41, Date: "2026-10-05"},
		{Supplier: "Hetzner Online GmbH", Account: 6540, Series: "A", Number: 77, Date: "2026-09-05"},
		{Supplier: "SAS Scandinavian Airlines", Account: 5810, Series: "A", Number: 12, Date: "2026-09-09"},
	}
}

func newHandler(t *testing.T, ex *fakeExtractor, opts ...func(*demo.Config)) http.Handler {
	t.Helper()
	cfg := demo.Config{
		History:   history(),
		Extractor: ex,
		Samples: fstest.MapFS{
			"001.png": {Data: pngBytes}, "002.png": {Data: pngBytes},
			// Present in the sample filesystem but not a sample: the name pattern, not
			// the filesystem lookup, must be what keeps these out.
			"secret.txt": {Data: []byte("not for the page")}, "9999.png": {Data: pngBytes}, "a.png": {Data: pngBytes},
		},
		MaxUpload: 1 << 20,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return demo.New(cfg)
}

func upload(t *testing.T, h http.Handler, data []byte, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("image", "inv.png")
	require.NoError(t, err)
	_, err = fw.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/process", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postForm(h http.Handler, path string, v url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHome_StatesTheDataIsSyntheticAndNothingIsPostedAnywhere(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, strings.ToLower(body), "synthetic")
	assert.Contains(t, strings.ToLower(body), "nothing is posted")
	assert.Contains(t, body, `name="image"`)
	assert.Contains(t, body, "001.png", "samples are listed")
}

func TestProcess_ShowsTheFieldsAndTheRankedProposalWithItsReason(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})

	rec := upload(t, h, pngBytes)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	for _, want := range []string{"Hetzner Online GmbH", "081000972341", "2026-11-02", "6540", "A41", "A77"} {
		assert.Contains(t, body, want)
	}
	assert.NotContains(t, strings.ToUpper(body), "DO NOT TRUST")
}

func TestProcess_ShowsDoNotTrustWhenTheAmountsDoNotReconcile(t *testing.T) {
	inv := good()
	inv.Total = 175 // a dropped digit
	h := newHandler(t, &fakeExtractor{inv: inv})

	rec := upload(t, h, pngBytes)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "DO NOT TRUST")
	assert.Contains(t, rec.Body.String(), "does not equal")
}

func TestProcess_WarnsThatAnAmbiguousDateCouldBeReadTwoWays(t *testing.T) {
	inv := good()
	inv.InvoiceDateAmbiguous = true
	h := newHandler(t, &fakeExtractor{inv: inv})

	rec := upload(t, h, pngBytes)

	assert.Contains(t, rec.Body.String(), "could be read two ways")
	assert.Contains(t, rec.Body.String(), "2026-11-02")
	assert.NotContains(t, rec.Body.String(), "DO NOT TRUST")
}

func TestProcess_ShowsAnErrorAndNoProposalWhenExtractionFails(t *testing.T) {
	h := newHandler(t, &fakeExtractor{err: errors.New("model down")})

	rec := upload(t, h, pngBytes)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "model down")
	assert.NotContains(t, rec.Body.String(), "6540")
}

func TestProcess_RefusesANonImageUpload(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})

	rec := upload(t, h, []byte("%PDF-1.7 not an image we accept"))

	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
}

func TestProcess_RefusesAnOversizeUpload(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()}, func(c *demo.Config) { c.MaxUpload = 1024 })

	rec := upload(t, h, append(append([]byte{}, pngBytes...), bytes.Repeat([]byte("y"), 4096)...))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestProcess_ProcessesAKnownSampleByName(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})

	rec := postForm(h, "/process", url.Values{"sample": {"001.png"}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Hetzner Online GmbH")
}

func TestProcess_RefusesASampleNameThatIsNotAPlainKnownFileName(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})
	for _, name := range []string{"../../etc/passwd", "..%2f001.png", "001.png/../002.png", "999.png", "", "001.PNG", "/001.png", "secret.txt", "9999.png", "a.png"} {
		t.Run(name, func(t *testing.T) {
			rec := postForm(h, "/process", url.Values{"sample": {name}})

			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestProcess_AnswersBusyInsteadOfQueueingASecondModelCall(t *testing.T) {
	ex := &fakeExtractor{inv: good(), gate: make(chan struct{}), in: make(chan struct{}, 1)}
	h := newHandler(t, ex)

	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- upload(t, h, pngBytes) }()
	<-ex.in // the first call is now inside the model

	second := upload(t, h, pngBytes)
	close(ex.gate)

	assert.Equal(t, http.StatusServiceUnavailable, second.Code)
	assert.Equal(t, http.StatusOK, (<-first).Code)
}

func TestFeedback_ACorrectionChangesTheNextProposalForThatSupplier(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})
	require.Contains(t, upload(t, h, pngBytes).Body.String(), "6540")

	for i := 0; i < 3; i++ {
		rec := postForm(h, "/feedback", url.Values{"supplier": {"Hetzner Online GmbH"}, "account": {"5420"}})
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Learned")
	}

	body := upload(t, h, pngBytes).Body.String()
	assert.Less(t, strings.Index(body, "5420"), strings.Index(body, "6540"), "the corrected account now ranks first")
}

func TestFeedback_RefusesAnAccountThatIsNotAFourDigitLedgerAccount(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})
	for _, acct := range []string{"", "abc", "12", "123456", "0999", "-6540", "6540; drop"} {
		t.Run(acct, func(t *testing.T) {
			rec := postForm(h, "/feedback", url.Values{"supplier": {"Hetzner Online GmbH"}, "account": {acct}})

			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestFeedback_ConcurrentCorrectionsAndProposalsDoNotRace(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			postForm(h, "/feedback", url.Values{"supplier": {"Hetzner Online GmbH"}, "account": {"6540"}})
		}()
		go func() {
			defer wg.Done()
			upload(t, h, pngBytes)
		}()
	}
	wg.Wait()
}

func TestProcess_EscapesUntrustedTextFromTheModel(t *testing.T) {
	inv := good()
	inv.Supplier = `<script>alert(1)</script>`
	h := newHandler(t, &fakeExtractor{inv: inv})

	rec := upload(t, h, pngBytes)

	assert.NotContains(t, rec.Body.String(), "<script>alert(1)</script>")
	assert.Contains(t, rec.Body.String(), "&lt;script&gt;")
}

func TestAuth_EveryRouteNeedsBasicAuthWhenCredentialsAreSet(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()}, func(c *demo.Config) { c.User, c.Pass = "demo", "s3cret" })

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/"}, {http.MethodPost, "/process"}, {http.MethodPost, "/feedback"}, {http.MethodGet, "/samples/001.png"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.NotEmpty(t, rec.Header().Get("WWW-Authenticate"))
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("demo", "wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("demo", "s3cret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestValidateExposure_RefusesANonLoopbackBindWithoutCredentials(t *testing.T) {
	tests := []struct {
		name, addr, user, pass string
		wantErr                bool
	}{
		{"loopback without credentials", "127.0.0.1:8099", "", "", false},
		{"localhost without credentials", "localhost:8099", "", "", false},
		{"ipv6 loopback without credentials", "[::1]:8099", "", "", false},
		{"all interfaces without credentials", ":8099", "", "", true},
		{"lan address without credentials", "10.0.1.20:8099", "", "", true},
		{"all interfaces with credentials", ":8099", "demo", "s3cret", false},
		{"lan address with only a user", "10.0.1.20:8099", "demo", "", true},
		{"lan address with only a password", "10.0.1.20:8099", "", "s3cret", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := demo.ValidateExposure(tt.addr, tt.user, tt.pass)

			assert.Equal(t, tt.wantErr, err != nil)
		})
	}
}

func TestSamples_ServesOnlyKnownSampleImages(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})

	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, "/samples/001.png", nil))
	assert.Equal(t, http.StatusOK, ok.Code)

	for _, path := range []string{"/samples/..%2f..%2fetc%2fpasswd", "/samples/secret.txt", "/samples/9999.png", "/samples/a.png"} {
		bad := httptest.NewRecorder()
		h.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, path, nil))
		assert.NotEqual(t, http.StatusOK, bad.Code, path)
	}
}

func TestValidateModelURL_AllowsOnlyAddressesOnThisNetwork(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"http://iguana:11434", false},
		{"http://localhost:11434", false},
		{"http://127.0.0.1:11434", false},
		{"http://10.0.1.10:11434", false},
		{"http://192.168.1.5:11434", false},
		{"http://172.16.4.2:11434", false},
		{"http://100.101.102.103:11434", false},
		{"http://iguana.tail30472e.ts.net:11434", false},
		{"https://api.openai.com", true},
		{"https://api.anthropic.com/v1", true},
		{"http://8.8.8.8:11434", true},
		{"http://172.32.0.1:11434", true},
		{"http://100.128.0.1:11434", true},
		{"http://example.com:11434", true},
		{"http://iguana.example.com:11434", true},
		{"not a url", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := demo.ValidateModelURL(tt.url)

			assert.Equal(t, tt.wantErr, err != nil)
		})
	}
}
