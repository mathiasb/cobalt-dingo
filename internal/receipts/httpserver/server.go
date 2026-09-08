// Package httpserver provides the HTTP admin server for coo-agent.
package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver/ui"
)

// Server is the HTTP server for coo-agent (admin UI + MCP endpoint).
type Server struct {
	srv *http.Server
}

// Config holds server configuration.
type Config struct {
	Addr   string // e.g. ":8080"
	Auth   Authenticator
	Tokens ui.TokenChecker
	Keys   ui.APIKeyStore
	Audit  ui.AuditReader
	Health ui.HealthChecker
}

// New constructs a Server wired with the provided dependencies.
//
// It returns ErrNoAuthenticator, and no Server, when cfg.Auth is nil. Returning
// nothing constructible is the point: a Server value that exists can be Started,
// and an unauthenticated admin server starts and serves without complaint.
func New(cfg Config) (*Server, error) {
	if cfg.Auth == nil {
		return nil, ErrNoAuthenticator
	}

	mux := http.NewServeMux()

	// Redirect root to UI.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Admin UI.
	uiHandler := ui.NewHandler(cfg.Tokens, cfg.Keys, cfg.Audit, cfg.Health)
	uiHandler.Register(mux, "/ui")

	// Everything registered above is behind the authenticator. The health probe
	// is registered on the outer mux below so a future admin route cannot be
	// added outside the guard by mistake — the default is protected.
	outer := http.NewServeMux()
	outer.HandleFunc("GET /health", healthHandler(cfg.Health))
	outer.Handle("/", requireAuth(cfg.Auth, mux))

	return &Server{
		srv: &http.Server{
			Addr:         cfg.Addr,
			Handler:      outer,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}, nil
}

// Handler exposes the fully wired handler, including the auth middleware, so
// tests exercise the same request path the listener serves.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Start begins listening. It returns when the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}
	slog.Info("httpserver listening", "addr", ln.Addr())

	errc := make(chan error, 1)
	go func() { errc <- s.srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutCtx)
	case err := <-errc:
		return err
	}
}

type healthResponse struct {
	DB      bool `json:"db"`
	Fortnox bool `json:"fortnox"`
	OK      bool `json:"ok"`
}

func healthHandler(h ui.HealthChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := healthResponse{
			DB:      h.DBReachable(r.Context()),
			Fortnox: h.FortnoxReachable(r.Context()),
		}
		resp.OK = resp.DB && resp.Fortnox
		status := http.StatusOK
		if !resp.OK {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
