// Command fortnox-auth performs the one-time OAuth2 authorization flow
// against either the sandbox or real_readonly Fortnox connected app and
// writes the resulting tokens to a mode-specific token file.
//
// Stop the dev server (task dev) before running this — both use port 8080.
//
// Mode selection (in order of precedence):
//
//  1. --mode CLI flag, e.g. --mode=sandbox or --mode=real_readonly
//  2. FORTNOX_MODE environment variable (Taskfile targets set this)
//
// If neither is set, fortnox-auth refuses to run rather than guessing.
//
// Usage:
//
//	source .env && go run ./cmd/fortnox-auth --mode=sandbox
//	source .env && go run ./cmd/fortnox-auth --mode=real_readonly
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	modeFlag := flag.String("mode", "",
		"Fortnox mode: sandbox or real_readonly. Overrides FORTNOX_MODE env if set.")
	flag.Parse()

	// Flag takes precedence over env. Setting the env var here keeps the
	// rest of the binary on the standard config.Load() path used by every
	// other Fortnox-touching command.
	if *modeFlag != "" {
		if err := os.Setenv("FORTNOX_MODE", *modeFlag); err != nil {
			log.Error("set FORTNOX_MODE from --mode flag", "err", err)
			os.Exit(1)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Error("config error", "err", err)
		os.Exit(1)
	}

	fmt.Printf("\n  Fortnox auth — mode: %s\n  Credential prefix : %s\n  Token file        : %s\n\n",
		cfg.Mode.Label(), cfg.Mode.EnvPrefix(), cfg.Mode.TokenFile())

	state, err := randomState()
	if err != nil {
		log.Error("generate state", "err", err)
		os.Exit(1)
	}

	authURL := fortnox.AuthURL(cfg.ClientID, cfg.RedirectURI, cfg.Scopes, state)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	callbackPath := extractPath(cfg.RedirectURI)
	srv := &http.Server{Addr: ":8080"}
	http.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch — possible CSRF", http.StatusBadRequest)
			errCh <- fmt.Errorf("state mismatch in callback")
			return
		}
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, errParam+": "+desc, http.StatusBadRequest)
			errCh <- fmt.Errorf("fortnox error %s: %s", errParam, desc)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code — full query: "+r.URL.RawQuery, http.StatusBadRequest)
			errCh <- fmt.Errorf("no code in callback, query: %s", r.URL.RawQuery)
			return
		}
		_, _ = w.Write([]byte("<html><body><h2>Authorization complete — you can close this tab.</h2></body></html>"))
		codeCh <- code
	})

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server: %w", err)
		}
	}()

	fmt.Println("Opening browser for Fortnox authorization...")
	fmt.Println("If the browser does not open, visit this URL manually:")
	fmt.Println(authURL)
	_ = openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		log.Error("auth failed", "err", err)
		os.Exit(1)
	}
	_ = srv.Shutdown(context.Background())

	token, err := fortnox.ExchangeCode(cfg.ClientID, cfg.ClientSecret, cfg.RedirectURI, code)
	if err != nil {
		log.Error("token exchange failed", "err", err)
		os.Exit(1)
	}

	if err := fortnox.SaveToken(cfg.Mode.TokenFile(), token); err != nil {
		log.Error("save token", "err", err)
		os.Exit(1)
	}

	log.Info("tokens saved", "expires_at", token.ExpiresAt.Format("2006-01-02 15:04:05"), "file", cfg.Mode.TokenFile())
	fmt.Printf("Done. Run: FORTNOX_MODE=%s go run ./cmd/fortnox-check\n", cfg.Mode)
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func extractPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "/callback"
	}
	return u.Path
}

func openBrowser(u string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{u}
	case "linux":
		cmd, args = "xdg-open", []string{u}
	default:
		return fmt.Errorf("unsupported OS for auto-open")
	}
	return exec.Command(cmd, args...).Start()
}
