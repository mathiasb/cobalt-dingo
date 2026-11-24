package demo_test

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

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

	onExtract func()      // when set, runs inside Extract
	panicOnce atomic.Bool // when armed, the next Extract panics (and disarms)
}

func (f *fakeExtractor) Extract(ctx context.Context, _ []byte) (extract.Invoice, error) {
	if f.panicOnce.CompareAndSwap(true, false) {
		panic("extractor blew up")
	}
	if f.onExtract != nil {
		f.onExtract()
	}
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
	assert.Contains(t, rec.Body.String(), "Could not read the invoice")
	assert.NotContains(t, rec.Body.String(), "model down", "the raw error is logged, not shown")
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

func TestAuth_FourRoutesNeedBasicAuthAndAWrongPasswordIsRefused(t *testing.T) {
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

func TestSamples_AKnownSampleIsServedAndFourOtherPathsAreNot200(t *testing.T) {
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

// It reads the text of the URL. It cannot know what a single-label name resolves
// to (a search domain could make "evil" public), so this is a guard against a
// mistyped or pasted cloud URL, not a boundary against someone who controls DNS.
func TestValidateModelURL_JudgesTheHostTextAndNotWhereItResolves(t *testing.T) {
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
		{"http://134744072", true},   // decimal form of 8.8.8.8
		{"http://0x08080808", true},  // hex form
		{"http://01002004010", true}, // octal form
		{"http://0", true},
		{"http://evil。com", true}, // ideographic full stop
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

// net/http spills multipart parts above the in-memory limit to os.TempDir() for
// the length of the handler. The page promises uploads are never stored.
func TestProcess_AnUploadOverOneMegabyteIsNotSpooledToDisk(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	var seen []string
	ex := &fakeExtractor{inv: good(), onExtract: func() {
		entries, _ := os.ReadDir(tmp)
		for _, e := range entries {
			seen = append(seen, e.Name())
		}
	}}
	h := newHandler(t, ex, func(c *demo.Config) { c.MaxUpload = 8 << 20 })
	big := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte("z"), 3<<20)...)

	rec := upload(t, h, big)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, seen, "no temp file may exist while the model is reading the image")
}

func TestCrossSite_APostFromAnotherOriginIsRefusedAndASameOriginPostIsNot(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})
	form := url.Values{"supplier": {"Hetzner Online GmbH"}, "account": {"5420"}}
	post := func(mutate func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/feedback", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mutate(req)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	cross := post(func(r *http.Request) {
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	})
	same := post(func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })

	assert.Equal(t, http.StatusForbidden, cross.Code)
	assert.Equal(t, http.StatusOK, same.Code)
}

func TestFeedback_LearningStopsAtTheCapAndTheResultPageStaysSmall(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()}, func(c *demo.Config) { c.MaxLearned = 50 })
	var refused int
	for i := 0; i < 500; i++ {
		rec := postForm(h, "/feedback", url.Values{"supplier": {"Hetzner Online GmbH"}, "account": {"6540"}})
		if rec.Code == http.StatusTooManyRequests {
			refused++
		}
	}

	rec := upload(t, h, pngBytes)

	assert.Equal(t, 450, refused, "every correction past the cap is refused, none silently dropped")
	assert.Less(t, rec.Body.Len(), 30_000, "past bookings shown are bounded however many exist")
	assert.Contains(t, rec.Body.String(), "more", "the page says how many were left out")
}

func TestProcess_DoesNotRevealTheModelEndpointOrErrorTextToTheBrowser(t *testing.T) {
	h := newHandler(t, &fakeExtractor{err: errors.New(`call model: Post "http://iguana:11434/api/chat": dial tcp 10.0.1.10:11434: refused`)})

	rec := upload(t, h, pngBytes)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.NotContains(t, rec.Body.String(), "iguana")
	assert.NotContains(t, rec.Body.String(), "11434")
	assert.NotContains(t, rec.Body.String(), "10.0.1.10")
	assert.Contains(t, rec.Body.String(), "Could not read the invoice")
}

func TestAuth_AWrongUserWithTheRightPasswordIsRefusedOnEveryMethod(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()}, func(c *demo.Config) { c.User, c.Pass = "demo", "s3cret" })

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, path := range []string{"/", "/process", "/feedback", "/samples/001.png", "/nope"} {
			t.Run(method+" "+path, func(t *testing.T) {
				req := httptest.NewRequest(method, path, nil)
				req.SetBasicAuth("someone-else", "s3cret")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)

				assert.Equal(t, http.StatusUnauthorized, rec.Code)
			})
		}
	}
}

func TestHeaders_EveryResponseCarriesTheCSPNosniffAndNoStore(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "default-src 'none'")
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestFeedback_ASupplierLongerThanTheLimitIsRefused(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})

	rec := postForm(h, "/feedback", url.Values{"supplier": {strings.Repeat("a", 201)}, "account": {"6540"}})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestProcess_AHungModelIsCutOffAtTheModelTimeoutAndTheSlotIsFreed(t *testing.T) {
	ex := &fakeExtractor{inv: good(), gate: make(chan struct{})} // never closed: the model hangs
	h := newHandler(t, ex, func(c *demo.Config) { c.ModelTimeout = 50 * time.Millisecond })

	start := time.Now()
	first := upload(t, h, pngBytes)

	assert.Equal(t, http.StatusBadGateway, first.Code)
	assert.Less(t, time.Since(start), 5*time.Second)
	ex.gate = nil
	second := upload(t, h, pngBytes)
	assert.Equal(t, http.StatusOK, second.Code, "the busy slot was released after the timeout")
}

func TestProcess_APanicInTheExtractorDoesNotLeaveTheModelSlotTaken(t *testing.T) {
	ex := &fakeExtractor{inv: good()}
	srv := httptest.NewServer(newHandler(t, ex))
	defer srv.Close()
	post := func() (int, error) {
		resp, err := http.PostForm(srv.URL+"/process", url.Values{"sample": {"001.png"}})
		if err != nil {
			return 0, err
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode, nil
	}
	ex.panicOnce.Store(true)

	_, err := post()
	require.Error(t, err, "the connection is dropped when the handler panics")
	code, err := post()

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, code, "not 503: the slot was released")
}

// With no cap on the request body, net/http spools everything above the memory
// limit to os.TempDir. A recorder-driven call has no server cleanup afterwards,
// so a leaked spool file is still visible when ServeHTTP returns.
func TestProcess_ABodyFarOverTheUploadLimitLeavesNoFileInTheTempDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	h := newHandler(t, &fakeExtractor{inv: good()}, func(c *demo.Config) { c.MaxUpload = 1 << 20 })
	huge := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte("q"), 3<<20)...)

	rec := upload(t, h, huge)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestProcess_TheBecauseListShowsTheNewestMatchesAndNotTheOldest(t *testing.T) {
	var many []booking.Example
	for i := 1; i <= 9; i++ {
		many = append(many, booking.Example{Supplier: "Hetzner Online GmbH", Account: 6540, Series: "A", Number: i * 100, Date: "2026-0" + strconv.Itoa(i) + "-01"})
	}
	h := newHandler(t, &fakeExtractor{inv: good()}, func(c *demo.Config) { c.History = many })

	body := upload(t, h, pngBytes).Body.String()

	for _, newest := range []string{"A900", "A800", "A700", "A600", "A500"} {
		assert.Contains(t, body, newest)
	}
	for _, oldest := range []string{"A100", "A200", "A300", "A400"} {
		assert.NotContains(t, body, oldest)
	}
	assert.Contains(t, body, "and 4 more")
}

func TestProcess_ALearnedCorrectionIsLabelledAsOneAndNotAsAVoucher(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})
	postForm(h, "/feedback", url.Values{"supplier": {"Hetzner Online GmbH"}, "account": {"6540"}})

	body := upload(t, h, pngBytes).Body.String()

	assert.Contains(t, body, "your correction")
	assert.NotContains(t, body, "voucher 0")
}

func TestFeedback_AnOversizeFormBodyIsRefusedBeforeItIsParsed(t *testing.T) {
	h := newHandler(t, &fakeExtractor{inv: good()})

	rec := postForm(h, "/feedback", url.Values{"supplier": {"Hetzner Online GmbH"}, "account": {"6540"}, "junk": {strings.Repeat("j", 64<<10)}})

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}
