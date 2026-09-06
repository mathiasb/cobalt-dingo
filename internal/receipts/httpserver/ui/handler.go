// Package ui provides HTTP handlers for the coo-agent admin UI.
package ui

import (
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver/ui/templates"
)

// Handler serves all /ui/* routes.
type Handler struct {
	tokens TokenChecker
	keys   APIKeyStore
	audit  AuditReader
	health HealthChecker
}

// NewHandler constructs a Handler from its dependencies.
func NewHandler(tokens TokenChecker, keys APIKeyStore, audit AuditReader, health HealthChecker) *Handler {
	return &Handler{tokens: tokens, keys: keys, audit: audit, health: health}
}

// Register mounts all UI routes onto mux under the given prefix (e.g. "/ui").
func (h *Handler) Register(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(prefix+"/", h.status)
	mux.HandleFunc(prefix+"/auth", h.auth)
	mux.HandleFunc("POST "+prefix+"/token/refresh", h.tokenRefresh)
	mux.HandleFunc("DELETE "+prefix+"/token", h.tokenRevoke)
	mux.HandleFunc(prefix+"/keys", h.keyList)
	mux.HandleFunc("GET "+prefix+"/keys/new", h.keyForm)
	mux.HandleFunc("POST "+prefix+"/keys", h.keyCreate)
	mux.HandleFunc("DELETE "+prefix+"/keys/{id}", h.keyRevoke)
	mux.HandleFunc(prefix+"/audit", h.auditLog)
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ts, _ := h.tokens.TokenStatus(r.Context())
	d := templates.StatusData{
		Token:   ts,
		DB:      h.health.DBReachable(r.Context()),
		Fortnox: h.health.FortnoxReachable(r.Context()),
	}
	render(w, r, templates.Status(d))
}

func (h *Handler) auth(w http.ResponseWriter, r *http.Request) {
	ts, _ := h.tokens.TokenStatus(r.Context())
	render(w, r, templates.Auth(ts))
}

func (h *Handler) tokenRefresh(w http.ResponseWriter, r *http.Request) {
	if err := h.tokens.RefreshToken(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts, _ := h.tokens.TokenStatus(r.Context())
	render(w, r, templates.AuthPartial(ts))
}

func (h *Handler) tokenRevoke(w http.ResponseWriter, r *http.Request) {
	if err := h.tokens.RevokeToken(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ts, _ := h.tokens.TokenStatus(r.Context())
	render(w, r, templates.AuthPartial(ts))
}

func (h *Handler) keyList(w http.ResponseWriter, r *http.Request) {
	ks, _ := h.keys.ListKeys(r.Context())
	render(w, r, templates.Keys(ks, nil))
}

func (h *Handler) keyForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.KeyForm())
}

func (h *Handler) keyCreate(w http.ResponseWriter, r *http.Request) {
	label := r.FormValue("label")
	if label == "" {
		http.Error(w, "etikett krävs", http.StatusBadRequest)
		return
	}
	created, err := h.keys.CreateKey(r.Context(), label)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// HTMX appends the new row to #key-list; the plaintext key is shown on
	// the full-page reload (redirect to /ui/keys with flash) in production.
	// For the pilot we render the row — the caller sees the key in the stub.
	render(w, r, templates.KeyRow(created.APIKey))
}

func (h *Handler) keyRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.keys.RevokeKey(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// HTMX swaps the row with empty content via outerHTML swap.
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) auditLog(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	events, _ := h.audit.ListEvents(r.Context(), page, 50)
	render(w, r, templates.Audit(events, page))
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = c.Render(r.Context(), w)
}
