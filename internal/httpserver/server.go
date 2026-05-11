package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/mathiasb/coo-agent/internal/httpserver/ui"
)

// Server is the HTTP server for coo-agent (admin UI + MCP endpoint).
type Server struct {
	srv *http.Server
}

// Config holds server configuration.
type Config struct {
	Addr    string // e.g. ":8080"
	Tokens  ui.TokenChecker
	Keys    ui.APIKeyStore
	Audit   ui.AuditReader
	Health  ui.HealthChecker
}

// New constructs a Server wired with the provided dependencies.
func New(cfg Config) *Server {
	mux := http.NewServeMux()

	// Health probe (used by k8s liveness/readiness).
	mux.HandleFunc("GET /health", healthHandler(cfg.Health))

	// Redirect root to UI.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Admin UI.
	uiHandler := ui.NewHandler(cfg.Tokens, cfg.Keys, cfg.Audit, cfg.Health)
	uiHandler.Register(mux, "/ui")

	return &Server{
		srv: &http.Server{
			Addr:         cfg.Addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}
}

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
