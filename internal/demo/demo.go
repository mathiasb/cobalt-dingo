// Package demo is a small server-rendered page for showing the invoice
// processor: pick a sample or upload an image, see what was read and a
// proposed booking with the past bookings behind it, and accept or correct it.
//
// It reads and proposes only. It has no Fortnox access of any kind, writes
// nothing to disk, and holds uploads in memory for the length of one request.
// The model is whatever Extractor it is given; point that at a local model.
package demo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/apentry"
	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/ingest"
	"github.com/mathiasb/cobalt-dingo/internal/process"
)

// Server timeouts, here so a test can check they fit together: a request's body
// read (ReadTimeout) and the model deadline (DefaultModelTimeout) both happen
// before the answer is written, and all of it must finish inside WriteTimeout,
// which net/http counts from the end of the request headers.
const (
	ReadTimeout         = 60 * time.Second
	WriteTimeout        = 4 * time.Minute
	DefaultModelTimeout = 150 * time.Second
)

const ( // multipartOverhead is slack for form framing above the file limit; the
	// file itself is checked against MaxUpload exactly.
	multipartOverhead = 64 << 10
	maxSupplierLen    = 200

	defaultMaxLearned = 200
	defaultMaxFiles   = 4
	defaultMaxParsing = 2
	entryStoreMax     = 20
	entryStoreTTL     = 30 * time.Minute
	becauseShown      = 5
)

var (
	sampleNameRe = regexp.MustCompile(`^[0-9]{3}\.(png|pdf|docx|xml)$`)
	accountRe    = regexp.MustCompile(`^[1-8][0-9]{3}$`)
	entryNameRe  = regexp.MustCompile(`^[0-9a-f]{32}\.csv$`)
)

// Config configures the demo handler.
type Config struct {
	History   []booking.Example
	Extractor process.Extractor
	Samples   fs.FS // flat directory of NNN.png|pdf|docx|xml sample invoices; may be nil
	MaxUpload int64 // largest accepted upload in bytes
	User      string
	Pass      string // both empty disables auth; ValidateExposure guards when that is allowed

	ModelTimeout time.Duration // zero means the default
	MaxLearned   int           // zero means the default

	MaxFiles       int              // files per submit; zero means the default
	MaxTotalUpload int64            // bytes across all files in one submit; zero means twice MaxUpload
	MaxParsing     int              // uploads being read at once; zero means the default
	Now            func() time.Time // for expiry; nil means time.Now
}

type server struct {
	cfg  Config
	busy chan struct{} // capacity one: a second model call is refused, not queued

	// parsing bounds uploads being read at once. Reading an upload costs memory
	// (about 23 MB per 8 MB file) before the model gate is reached.
	parsing chan struct{}
	entries *entryStore

	mu      sync.RWMutex // guards prop and learned: Learn writes, Propose reads
	prop    *booking.Proposer
	learned int

	page *template.Template
}

// New returns the demo handler.
func New(cfg Config) http.Handler {
	if cfg.ModelTimeout <= 0 {
		cfg.ModelTimeout = DefaultModelTimeout
	}
	if cfg.MaxLearned <= 0 {
		cfg.MaxLearned = defaultMaxLearned
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = defaultMaxFiles
	}
	if cfg.MaxTotalUpload <= 0 {
		cfg.MaxTotalUpload = 2 * cfg.MaxUpload
	}
	if cfg.MaxParsing <= 0 {
		cfg.MaxParsing = defaultMaxParsing
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &server{
		cfg:     cfg,
		busy:    make(chan struct{}, 1),
		parsing: make(chan struct{}, cfg.MaxParsing),
		entries: newEntryStore(cfg.Now, entryStoreMax, entryStoreTTL),
		prop:    booking.NewProposer(cfg.History),
		page: template.Must(template.New("page").Funcs(template.FuncMap{
			"money": money, "pct": pct, "amt": amt, "inc": func(i int) int { return i + 1 },
			"isImage": func(name string) bool { return strings.HasSuffix(name, ".png") },
		}).Parse(pageHTML)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("POST /process", s.processHandler)
	mux.HandleFunc("POST /feedback", s.feedback)
	mux.HandleFunc("GET /samples/{name}", s.sample)
	mux.HandleFunc("GET /entries/{name}", s.entriesCSV)
	// Browsers send cached basic-auth credentials on cross-site form posts, so
	// state-changing requests from another origin are refused.
	return s.guard(http.NewCrossOriginProtection().Handler(mux))
}

// ValidateExposure refuses to serve beyond loopback without credentials, and
// refuses half a credential pair anywhere. The proxy in front of this adds
// TLS; it does not add a login.
func ValidateExposure(addr, user, pass string) error {
	if (user == "") != (pass == "") {
		return errors.New("set both the user and the password, or neither")
	}
	if user != "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad listen address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q without credentials: set a user and password, or bind to loopback", addr)
}

func (s *server) guard(next http.Handler) http.Handler {
	wantU := sha256.Sum256([]byte(s.cfg.User))
	wantP := sha256.Sum256([]byte(s.cfg.Pass))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
		if s.cfg.User != "" {
			u, p, ok := r.BasicAuth()
			gotU, gotP := sha256.Sum256([]byte(u)), sha256.Sum256([]byte(p))
			// Both comparisons always run, so timing does not reveal which one failed.
			okU := subtle.ConstantTimeCompare(gotU[:], wantU[:]) == 1
			okP := subtle.ConstantTimeCompare(gotP[:], wantP[:]) == 1
			if !ok || !okU || !okP {
				h.Set("WWW-Authenticate", `Basic realm="invoice demo"`)
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// item is the outcome for one invoice in a submit.
type item struct {
	Name          string // sample file name, or the position of an upload
	Result        *process.Result
	Because       []booking.Example // the most recent few, not all of them
	More          int               // how many past bookings were left out of Because
	Supplier      string
	Entry         *apentry.Entry
	EntryWarnings []string // the entry's warnings that the reading does not already show
	NoEntry       string   // why there is no entry
	Error         string
}

type view struct {
	Samples   []string
	Items     []item
	EntriesID string
	Notice    string
	Error     string
}

func (s *server) sampleNames() []string {
	if s.cfg.Samples == nil {
		return nil
	}
	names, _ := fs.Glob(s.cfg.Samples, "*")
	out := names[:0]
	for _, n := range names {
		if sampleNameRe.MatchString(n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (s *server) render(w http.ResponseWriter, code int, v view) {
	v.Samples = s.sampleNames()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := s.page.Execute(w, v); err != nil {
		// Headers are gone; the client gets a truncated page and we get a log line.
		slog.Warn("render demo page", "error", err)
	}
}

func (s *server) home(w http.ResponseWriter, _ *http.Request) { s.render(w, http.StatusOK, view{}) }

func (s *server) sample(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.cfg.Samples == nil || !sampleNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.cfg.Samples, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := name[strings.LastIndex(name, "."):]
	w.Header().Set("Content-Type", sampleTypes[ext])
	if ext != ".png" {
		// A document is offered for download so a visitor can try the upload with it,
		// never rendered in the page.
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	_, _ = w.Write(data)
}

var sampleTypes = map[string]string{
	".png":  "image/png",
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xml":  "application/xml",
}

// input is one invoice image to read: a sample or an upload held in memory.
type input struct {
	name string
	data []byte
}

// inputs reads the submitted samples and uploads. On refusal it has already
// written the response. Every refusal happens before anything reaches the model.
func (s *server) inputs(w http.ResponseWriter, r *http.Request) ([]input, bool) {
	limit := s.cfg.MaxTotalUpload + multipartOverhead
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	// The memory limit must cover the largest accepted request: above it,
	// net/http spools parts to os.TempDir(), and uploads are never to touch disk.
	if err := r.ParseMultipartForm(limit); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.render(w, http.StatusRequestEntityTooLarge, view{Error: "That upload is too large."})
			return nil, false
		}
		if err := r.ParseForm(); err != nil {
			s.render(w, http.StatusBadRequest, view{Error: "Could not read the form."})
			return nil, false
		}
	}

	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		files = r.MultipartForm.File["image"]
	}
	samples := r.Form["sample"]
	if len(files)+len(samples) == 0 {
		s.render(w, http.StatusBadRequest, view{Error: "Choose a listed sample or upload an image."})
		return nil, false
	}
	if len(files)+len(samples) > s.cfg.MaxFiles {
		s.render(w, http.StatusBadRequest, view{Error: fmt.Sprintf("At most %d invoices at a time.", s.cfg.MaxFiles)})
		return nil, false
	}

	var out []input
	var total int64
	for _, name := range samples {
		if s.cfg.Samples == nil || !sampleNameRe.MatchString(name) {
			s.render(w, http.StatusBadRequest, view{Error: "Choose a listed sample or upload an image."})
			return nil, false
		}
		b, err := fs.ReadFile(s.cfg.Samples, name)
		if err != nil {
			s.render(w, http.StatusBadRequest, view{Error: "Unknown sample."})
			return nil, false
		}
		out = append(out, input{name: name, data: b})
	}
	for i, fh := range files {
		f, err := fh.Open()
		if err != nil {
			s.render(w, http.StatusBadRequest, view{Error: "Could not read the upload."})
			return nil, false
		}
		b, err := io.ReadAll(io.LimitReader(f, s.cfg.MaxUpload+1))
		_ = f.Close()
		if err != nil {
			s.render(w, http.StatusBadRequest, view{Error: "Could not read the upload."})
			return nil, false
		}
		if int64(len(b)) > s.cfg.MaxUpload {
			s.render(w, http.StatusRequestEntityTooLarge, view{Error: "That upload is too large."})
			return nil, false
		}
		total += int64(len(b))
		if total > s.cfg.MaxTotalUpload {
			s.render(w, http.StatusRequestEntityTooLarge, view{Error: "Those uploads are too large together."})
			return nil, false
		}
		// By content, not by the file's name or the browser's Content-Type.
		if ingest.Sniff(b) == ingest.KindUnknown {
			s.render(w, http.StatusUnsupportedMediaType, view{Error: ingest.ErrUnsupported.Error()})
			return nil, false
		}
		out = append(out, input{name: fmt.Sprintf("upload %d", i+1), data: b})
	}
	return out, true
}

func (s *server) processHandler(w http.ResponseWriter, r *http.Request) {
	select {
	case s.parsing <- struct{}{}:
	default:
		s.render(w, http.StatusServiceUnavailable, view{Error: "Too many uploads are arriving at once. Try again in a moment."})
		return
	}
	// The slot is released by defer so a panic while reading cannot keep it.
	ins, ok := func() ([]input, bool) {
		defer func() { <-s.parsing }()
		return s.inputs(w, r)
	}()
	if !ok {
		return
	}

	select {
	case s.busy <- struct{}{}:
		defer func() { <-s.busy }()
	default:
		s.render(w, http.StatusServiceUnavailable, view{Error: "The model is busy with another invoice. Try again in a moment."})
		return
	}

	// One deadline for the whole submit, so a batch cannot outlast the server's
	// write timeout; invoices that do not fit in it say so.
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ModelTimeout)
	defer cancel()

	items := make([]item, 0, len(ins))
	var entries []apentry.Entry
	failed, documentFault := 0, 0
	for _, in := range ins {
		it := item{Name: in.name}
		if ctx.Err() != nil {
			it.Error = "Skipped: this submit ran out of time."
			failed++
			items = append(items, it)
			continue
		}
		inv, err := s.cfg.Extractor.Extract(ctx, in.data)
		if err != nil {
			// The detail can name the model host, its error text or the document's own
			// text: log it, show only a fixed sentence.
			slog.Warn("demo: extraction failed", "error", err)
			var mine bool
			it.Error, mine = readFailure(err)
			if mine {
				documentFault++
			}
			failed++
			items = append(items, it)
			continue
		}

		s.mu.RLock()
		res := process.Assess(inv, s.prop)
		s.mu.RUnlock()
		it.Result, it.Supplier = &res, inv.Supplier
		it.Because = res.Proposal.Because
		if len(it.Because) > becauseShown {
			it.More = len(it.Because) - becauseShown
			it.Because = it.Because[len(it.Because)-becauseShown:]
		}
		if e, err := apentry.Build(res); err != nil {
			it.NoEntry = err.Error()
		} else {
			it.Entry = &e
			it.EntryWarnings = newWarnings(e.Warnings, res.Warnings)
			entries = append(entries, e)
		}
		items = append(items, it)
	}

	v := view{Items: items}
	if len(entries) > 0 {
		v.EntriesID = s.entries.put(entries)
	}
	code := http.StatusOK
	if failed == len(items) {
		// Every document failed. If each failure was the document's, say so; if any
		// was the model's, the server is what to retry.
		code = http.StatusBadGateway
		if documentFault == failed {
			code = http.StatusUnprocessableEntity
		}
	}
	s.render(w, code, v)
}

// readFailure is the sentence shown for a document that could not be read, and
// whether the document (not the model) is to blame.
func readFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, ingest.ErrScanned):
		return ingest.ErrScanned.Error() + ".", true
	case errors.Is(err, ingest.ErrRefused):
		return "This file could not be read: it is malformed, empty, too large, or not in a layout this demo reads.", true
	case errors.Is(err, ingest.ErrUnsupported):
		return ingest.ErrUnsupported.Error() + ".", true
	default:
		return "Could not read the invoice. The model may be busy or unreachable; try again.", false
	}
}

// newWarnings returns the warnings in have that are not already in shown.
func newWarnings(have, shown []string) []string {
	var out []string
	for _, w := range have {
		dup := false
		for _, x := range shown {
			if w == x {
				dup = true
			}
		}
		if !dup {
			out = append(out, w)
		}
	}
	return out
}

// entriesCSV serves the entries of one earlier submit. The id is 128 random bits
// kept in memory for a short time; the login guard wraps this like every route.
func (s *server) entriesCSV(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !entryNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	entries, ok := s.entries.get(strings.TrimSuffix(name, ".csv"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ap-entries-proposed.csv"`)
	if err := apentry.WriteCSV(w, entries); err != nil {
		slog.Warn("demo: write csv", "error", err)
	}
}

// entryStore keeps proposed entries in memory only, bounded in count and age.
type entryStore struct {
	mu    sync.Mutex
	now   func() time.Time
	max   int
	ttl   time.Duration
	byID  map[string]storedEntries
	order []string
}

type storedEntries struct {
	entries []apentry.Entry
	at      time.Time
}

func newEntryStore(now func() time.Time, max int, ttl time.Duration) *entryStore {
	return &entryStore{now: now, max: max, ttl: ttl, byID: map[string]storedEntries{}}
}

func (e *entryStore) put(entries []apentry.Entry) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand does not fail on supported platforms; if it ever does, do
		// not hand out a guessable id.
		panic(fmt.Sprintf("demo: crypto/rand: %v", err))
	}
	id := hex.EncodeToString(raw[:])

	e.mu.Lock()
	defer e.mu.Unlock()
	e.pruneLocked()
	e.byID[id] = storedEntries{entries: entries, at: e.now()}
	e.order = append(e.order, id)
	for len(e.order) > e.max {
		delete(e.byID, e.order[0])
		e.order = e.order[1:]
	}
	return id
}

func (e *entryStore) get(id string) ([]apentry.Entry, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pruneLocked()
	st, ok := e.byID[id]
	return st.entries, ok
}

func (e *entryStore) pruneLocked() {
	cutoff := e.now().Add(-e.ttl)
	keep := e.order[:0]
	for _, id := range e.order {
		if e.byID[id].at.Before(cutoff) {
			delete(e.byID, id)
			continue
		}
		keep = append(keep, id)
	}
	e.order = keep
}

// maxFeedbackBody bounds a correction form: a supplier and a four-digit account
// need a few hundred bytes. Without it a junk field is parsed up to net/http's
// 10 MB default and the supplier is still learned.
const maxFeedbackBody = 8 << 10

func (s *server) feedback(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFeedbackBody)
	if err := r.ParseForm(); err != nil {
		s.render(w, http.StatusBadRequest, view{Error: "Could not read the form."})
		return
	}
	supplier := r.PostFormValue("supplier")
	account := r.PostFormValue("account")
	if supplier == "" || len(supplier) > maxSupplierLen || !accountRe.MatchString(account) {
		s.render(w, http.StatusBadRequest, view{Error: "A supplier and a four-digit ledger account are needed."})
		return
	}
	n, _ := strconv.Atoi(account)

	s.mu.Lock()
	if s.learned >= s.cfg.MaxLearned {
		s.mu.Unlock()
		s.render(w, http.StatusTooManyRequests, view{Error: "This demo has reached its limit of corrections. Restart it to clear them."})
		return
	}
	s.prop.Learn(booking.Example{Supplier: supplier, Account: n})
	s.learned++
	s.mu.Unlock()

	s.render(w, http.StatusOK, view{Notice: fmt.Sprintf("Learned: %s is booked to %d. Process it again to see the proposal change.", supplier, n)})
}

func money(minor int64, cur string) string {
	neg := ""
	if minor < 0 {
		neg, minor = "-", -minor
	}
	return fmt.Sprintf("%s%d.%02d %s", neg, minor/100, minor%100, cur)
}

func amt(minor int64) string {
	if minor == 0 {
		return ""
	}
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

func pct(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }

const pageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Invoice processor demo</title>
<style>
 body{font:16px/1.45 system-ui,sans-serif;max-width:60rem;margin:1.5rem auto;padding:0 1rem;color:#1b1f23}
 .banner{background:#fff4ce;border:1px solid #d9b100;padding:.6rem .9rem;border-radius:6px}
 .bad{background:#fde7e9;border:2px solid #c50f1f;padding:.7rem .9rem;border-radius:6px;font-weight:600}
 .warn{background:#fff4ce;border:1px solid #d9b100;padding:.5rem .9rem;border-radius:6px;margin:.5rem 0}
 .ok{background:#e6f4ea;border:1px solid #1e7e34;padding:.5rem .9rem;border-radius:6px}
 table{border-collapse:collapse;margin:.5rem 0} td,th{border:1px solid #c8ccd0;padding:.3rem .6rem;text-align:left}
 .samples{display:flex;gap:.6rem;flex-wrap:wrap} .samples form{border:1px solid #c8ccd0;padding:.4rem;border-radius:6px}
 .samples img{width:90px;display:block} button{font:inherit;padding:.3rem .7rem;cursor:pointer}
 .untrusted{opacity:.6}
</style></head><body>
<h1>Invoice processor demo</h1>
<p class="banner"><strong>Synthetic data.</strong> The past bookings behind these proposals are made up, so the accuracy
you see says nothing about any real company's books. <strong>Nothing is posted anywhere:</strong> this page has no
connection to any accounting system. Images are read by a model on the local network, held in memory for one request
and never stored. Accounts payable entries shown here are proposed only and not posted;
the CSV is a file you choose what to do with.</p>

{{if .Notice}}<p class="ok">{{.Notice}}</p>{{end}}
{{if .Error}}<p class="bad">{{.Error}}</p>{{end}}

<h2>1. Choose an invoice</h2>
<form method="post" action="/process" enctype="multipart/form-data">
 <p><label>Upload invoices: <input type="file" name="image" accept="image/png,image/jpeg,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/xml,text/xml,.png,.jpg,.jpeg,.pdf,.docx,.xml" multiple></label>
 <button type="submit">Read it</button></p>
<p class="note">Reads PNG and JPEG images, PDF files with a text layer, Word (.docx) documents and e-invoice XML (Peppol/UBL). A scanned PDF has no text to read: export it as a PNG or JPEG.</p>
</form>
{{if .Samples}}<p>Or a sample:</p><div class="samples">
{{range .Samples}}<form method="post" action="/process">{{if isImage .}}<img src="/samples/{{.}}" alt="sample {{.}}">{{else}}<a class="doc" href="/samples/{{.}}" download>download {{.}}</a>{{end}}
 <button name="sample" value="{{.}}">{{.}}</button></form>{{end}}</div>{{end}}

{{if .EntriesID}}<p><a href="/entries/{{.EntriesID}}.csv">Download all proposed entries (CSV)</a></p>{{end}}
{{range $i, $it := .Items}}
<h2>Invoice {{inc $i}}{{if $it.Name}}: {{$it.Name}}{{end}}</h2>
{{if $it.Error}}<p class="bad">{{$it.Error}}</p>{{end}}
{{with $it.Result}}
<h3>What was read</h3>
{{if .DoNotTrust}}<p class="bad">DO NOT TRUST this reading: the amounts do not add up. Check the invoice by hand.</p>{{end}}
{{range .Warnings}}<p class="warn">{{.}}</p>{{end}}
<table>
 <tr><th>Supplier</th><td>{{.Invoice.Supplier}}</td></tr>
 <tr><th>Invoice number</th><td>{{.Invoice.InvoiceNumber}}</td></tr>
 <tr><th>Invoice date</th><td>{{.Invoice.InvoiceDate}}{{if .Invoice.InvoiceDateAmbiguous}} (could be read two ways){{end}}</td></tr>
 {{if .Invoice.DueDate}}<tr><th>Due date</th><td>{{.Invoice.DueDate}}{{if .Invoice.DueDateAmbiguous}} (could be read two ways){{end}}</td></tr>{{end}}
 {{if .Invoice.NetKnown}}<tr><th>Net</th><td>{{money .Invoice.NetTotal .Invoice.Currency}}</td></tr>{{end}}
 <tr><th>VAT</th><td>{{money .Invoice.VATAmount .Invoice.Currency}}</td></tr>
 <tr><th>Total</th><td>{{money .Invoice.Total .Invoice.Currency}}</td></tr>
</table>

<h3>Proposed booking{{if .DoNotTrust}} (marked: do not trust){{end}}</h3>
{{if .Proposal.Candidates}}
<div{{if .DoNotTrust}} class="untrusted"{{end}}>
<p>Confidence {{pct .Proposal.Confidence}}.{{if .Proposal.VATAccount}} Input VAT account used before: {{.Proposal.VATAccount}}.{{end}}</p>
<table><tr><th>Expense account</th><th>Share of votes</th><th></th></tr>
{{range .Proposal.Candidates}}<tr><td>{{.Account}}</td><td>{{pct .Score}}</td><td>
 <form method="post" action="/feedback"><input type="hidden" name="supplier" value="{{$it.Supplier}}">
 <input type="hidden" name="account" value="{{.Account}}"><button>Accept {{.Account}}</button></form></td></tr>{{end}}
</table>
<p>Why: the same supplier was booked this way before:</p>
<ul>{{range $it.Because}}<li>{{if .Series}}voucher {{.Series}}{{.Number}}{{if .Date}}, {{.Date}}{{end}}{{else}}your correction{{end}}: account {{.Account}}</li>{{end}}
{{if $it.More}}<li>and {{$it.More}} more like these</li>{{end}}</ul>
</div>
<form method="post" action="/feedback"><input type="hidden" name="supplier" value="{{.Invoice.Supplier}}">
 <label>Or correct it to account <input name="account" size="5" inputmode="numeric" pattern="[1-8][0-9]{3}"></label>
 <button>Correct</button></form>
{{else}}<p>This supplier has not been seen before, so no booking is proposed. Guessing is worse than saying so.</p>
<form method="post" action="/feedback"><input type="hidden" name="supplier" value="{{.Invoice.Supplier}}">
 <label>Teach it: book this supplier to account <input name="account" size="5" inputmode="numeric" pattern="[1-8][0-9]{3}"></label>
 <button>Learn</button></form>{{end}}

<h3>Proposed accounts payable entry (not posted)</h3>
{{if $it.Entry}}
{{range $it.EntryWarnings}}<p class="warn">{{.}}</p>{{end}}
<p>{{$it.Entry.Date}}: {{$it.Entry.Text}} ({{$it.Entry.Currency}})</p>
<table><tr><th>Account</th><th>Debit</th><th>Credit</th></tr>
{{range $it.Entry.Lines}}<tr><td>{{.Account}}</td><td>{{amt .Debit}}</td><td>{{amt .Credit}}</td></tr>{{end}}
</table>
{{else}}<p class="warn">{{$it.NoEntry}}</p>{{end}}
{{end}}
{{end}}
</body></html>
`
