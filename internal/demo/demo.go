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
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/process"
)

const (
	modelTimeout = 3 * time.Minute
	// multipartOverhead is slack for form framing above the file limit; the
	// file itself is checked against MaxUpload exactly.
	multipartOverhead = 64 << 10
	maxSupplierLen    = 200
)

var (
	sampleNameRe = regexp.MustCompile(`^[0-9]{3}\.png$`)
	accountRe    = regexp.MustCompile(`^[1-8][0-9]{3}$`)
)

// Config configures the demo handler.
type Config struct {
	History   []booking.Example
	Extractor process.Extractor
	Samples   fs.FS // flat directory of NNN.png sample invoices; may be nil
	MaxUpload int64 // largest accepted upload in bytes
	User      string
	Pass      string // both empty disables auth; ValidateExposure guards when that is allowed
}

type server struct {
	cfg  Config
	busy chan struct{} // capacity one: a second model call is refused, not queued

	mu   sync.RWMutex // guards prop: Learn writes, Propose reads
	prop *booking.Proposer

	page *template.Template
}

// New returns the demo handler.
func New(cfg Config) http.Handler {
	s := &server{
		cfg:  cfg,
		busy: make(chan struct{}, 1),
		prop: booking.NewProposer(cfg.History),
		page: template.Must(template.New("page").Funcs(template.FuncMap{
			"money": money, "pct": pct,
		}).Parse(pageHTML)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("POST /process", s.processHandler)
	mux.HandleFunc("POST /feedback", s.feedback)
	mux.HandleFunc("GET /samples/{name}", s.sample)
	return s.guard(mux)
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

type view struct {
	Samples  []string
	Result   *process.Result
	Because  []booking.Example
	Notice   string
	Error    string
	Supplier string
	Sample   string
}

func (s *server) sampleNames() []string {
	if s.cfg.Samples == nil {
		return nil
	}
	names, _ := fs.Glob(s.cfg.Samples, "*.png")
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
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(data)
}

// image returns the bytes to process: a named sample, or an uploaded file held
// in memory. On refusal it has already written the response.
func (s *server) image(w http.ResponseWriter, r *http.Request) (data []byte, sample string, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUpload+multipartOverhead)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.render(w, http.StatusRequestEntityTooLarge, view{Error: "That upload is too large."})
			return nil, "", false
		}
		if err := r.ParseForm(); err != nil {
			s.render(w, http.StatusBadRequest, view{Error: "Could not read the form."})
			return nil, "", false
		}
	}

	if name := r.FormValue("sample"); name != "" || r.MultipartForm == nil || len(r.MultipartForm.File["image"]) == 0 {
		if s.cfg.Samples == nil || !sampleNameRe.MatchString(name) {
			s.render(w, http.StatusBadRequest, view{Error: "Choose a listed sample or upload an image."})
			return nil, "", false
		}
		b, err := fs.ReadFile(s.cfg.Samples, name)
		if err != nil {
			s.render(w, http.StatusBadRequest, view{Error: "Unknown sample."})
			return nil, "", false
		}
		return b, name, true
	}

	f, _, err := r.FormFile("image")
	if err != nil {
		s.render(w, http.StatusBadRequest, view{Error: "Could not read the upload."})
		return nil, "", false
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, s.cfg.MaxUpload+1))
	if err != nil {
		s.render(w, http.StatusBadRequest, view{Error: "Could not read the upload."})
		return nil, "", false
	}
	if int64(len(b)) > s.cfg.MaxUpload {
		s.render(w, http.StatusRequestEntityTooLarge, view{Error: "That upload is too large."})
		return nil, "", false
	}
	switch http.DetectContentType(b) {
	case "image/png", "image/jpeg":
		return b, "", true
	}
	s.render(w, http.StatusUnsupportedMediaType, view{Error: "Only PNG and JPEG images are accepted."})
	return nil, "", false
}

func (s *server) processHandler(w http.ResponseWriter, r *http.Request) {
	data, sample, ok := s.image(w, r)
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

	ctx, cancel := context.WithTimeout(r.Context(), modelTimeout)
	defer cancel()
	inv, err := s.cfg.Extractor.Extract(ctx, data)
	if err != nil {
		s.render(w, http.StatusBadGateway, view{Error: fmt.Sprintf("Could not read the invoice: %v", err)})
		return
	}

	s.mu.RLock()
	res := process.Assess(inv, s.prop)
	s.mu.RUnlock()
	s.render(w, http.StatusOK, view{Result: &res, Because: res.Proposal.Because, Sample: sample, Supplier: inv.Supplier})
}

func (s *server) feedback(w http.ResponseWriter, r *http.Request) {
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
	s.prop.Learn(booking.Example{Supplier: supplier, Account: n})
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
and never stored.</p>

{{if .Notice}}<p class="ok">{{.Notice}}</p>{{end}}
{{if .Error}}<p class="bad">{{.Error}}</p>{{end}}

<h2>1. Choose an invoice</h2>
<form method="post" action="/process" enctype="multipart/form-data">
 <p><label>Upload a PNG or JPEG: <input type="file" name="image" accept="image/png,image/jpeg"></label>
 <button type="submit">Read it</button></p>
</form>
{{if .Samples}}<p>Or a sample:</p><div class="samples">
{{range .Samples}}<form method="post" action="/process"><img src="/samples/{{.}}" alt="sample {{.}}">
 <button name="sample" value="{{.}}">{{.}}</button></form>{{end}}</div>{{end}}

{{with .Result}}
<h2>2. What was read</h2>
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

<h2>3. Proposed booking{{if .DoNotTrust}} (marked: do not trust){{end}}</h2>
{{if .Proposal.Candidates}}
<div{{if .DoNotTrust}} class="untrusted"{{end}}>
<p>Confidence {{pct .Proposal.Confidence}}.{{if .Proposal.VATAccount}} Input VAT account used before: {{.Proposal.VATAccount}}.{{end}}</p>
<table><tr><th>Expense account</th><th>Share of votes</th><th></th></tr>
{{range .Proposal.Candidates}}<tr><td>{{.Account}}</td><td>{{pct .Score}}</td><td>
 <form method="post" action="/feedback"><input type="hidden" name="supplier" value="{{$.Supplier}}">
 <input type="hidden" name="account" value="{{.Account}}"><button>Accept {{.Account}}</button></form></td></tr>{{end}}
</table>
<p>Why: the same supplier was booked this way before:</p>
<ul>{{range .Proposal.Because}}<li>voucher {{.Series}}{{.Number}}{{if .Date}}, {{.Date}}{{end}}: account {{.Account}}</li>{{end}}</ul>
</div>
<form method="post" action="/feedback"><input type="hidden" name="supplier" value="{{.Invoice.Supplier}}">
 <label>Or correct it to account <input name="account" size="5" inputmode="numeric" pattern="[1-8][0-9]{3}"></label>
 <button>Correct</button></form>
{{else}}<p>This supplier has not been seen before, so no booking is proposed. Guessing is worse than saying so.</p>
<form method="post" action="/feedback"><input type="hidden" name="supplier" value="{{.Invoice.Supplier}}">
 <label>Teach it: book this supplier to account <input name="account" size="5" inputmode="numeric" pattern="[1-8][0-9]{3}"></label>
 <button>Learn</button></form>{{end}}
{{end}}
</body></html>
`
