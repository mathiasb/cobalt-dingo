// Command demo serves the invoice processor demo page.
//
//	DEMO_USER=demo DEMO_PASS=... go run ./cmd/demo
//
// Environment:
//
//	DEMO_ADDR        listen address (default 127.0.0.1:8099). Anything that is not
//	                 loopback needs DEMO_USER and DEMO_PASS, or startup is refused.
//	DEMO_DATA        directory made by scripts/gen-synthetic-invoices.py (default testdata/synthetic)
//	DEMO_MODEL_URL   Ollama-compatible endpoint on the local network (default http://iguana:11434)
//	DEMO_MODEL       vision model (default qwen2.5vl:7b)
//	DEMO_FX          exchange-rate file for SEK conversion (default <DEMO_DATA>/fx-rates.json), made by
//	                 scripts/fetch-riksbank-rates.py; absent or invalid means foreign entries stay in
//	                 the invoice currency and say so
//	DEMO_TEXT_MODEL  model that reads text from PDF and Word files (default: DEMO_MODEL)
//	DEMO_MARKITDOWN  path of the markitdown binary for PDF and Word (default: markitdown on PATH; install it in a virtualenv, see docs/demo-runbook.md)
//	DEMO_DATE_ORDER  dmy (default) or mdy: how 02.11.2026 is read when both readings are valid
//
// It has no accounting-system access and writes nothing to disk. Uploads are
// held in memory; the PDF and Word converter receives them on stdin.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/demo"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/fx"
	"github.com/mathiasb/cobalt-dingo/internal/ingest"
)

// loadRates reads the exchange-rate file. A missing file is normal (conversion is
// off); an invalid one is refused whole and logged, and conversion stays off: a
// partly trusted rate table would be worse than none.
func loadRates(log *slog.Logger, path string) *fx.Table {
	f, err := os.Open(path)
	if err != nil {
		log.Info("no exchange-rate file: foreign-currency entries stay in the invoice currency", "path", path)
		return nil
	}
	defer func() { _ = f.Close() }()
	tb, err := fx.Load(f)
	if err != nil {
		log.Error("exchange-rate file refused: foreign-currency entries stay in the invoice currency", "path", path, "reason", err.Error())
		return nil
	}
	log.Info("exchange rates loaded", "path", path, "currencies", tb.Currencies())
	return tb
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("demo failed to start", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	addr := env("DEMO_ADDR", "127.0.0.1:8099")
	data := env("DEMO_DATA", "testdata/synthetic")
	modelURL := env("DEMO_MODEL_URL", "http://iguana:11434")
	model := env("DEMO_MODEL", "qwen2.5vl:7b")
	textModel := env("DEMO_TEXT_MODEL", model)
	converter := ingest.NewMarkitdown()
	if v := os.Getenv("DEMO_MARKITDOWN"); v != "" {
		converter.Command = strings.Fields(v)
	}
	user, pass := os.Getenv("DEMO_USER"), os.Getenv("DEMO_PASS")

	if err := demo.ValidateExposure(addr, user, pass); err != nil {
		return err
	}
	if err := demo.ValidateModelURL(modelURL); err != nil {
		return err
	}
	order := extract.DMY
	switch v := env("DEMO_DATE_ORDER", "dmy"); v {
	case "dmy":
	case "mdy":
		order = extract.MDY
	default:
		return fmt.Errorf("DEMO_DATE_ORDER must be dmy or mdy, got %q", v)
	}

	raw, err := os.ReadFile(filepath.Join(data, "history.json"))
	if err != nil {
		return fmt.Errorf("read history (run: uv run scripts/gen-synthetic-invoices.py): %w", err)
	}
	var vouchers []domain.Voucher
	if err := json.Unmarshal(raw, &vouchers); err != nil {
		return fmt.Errorf("parse history: %w", err)
	}
	history := booking.ExamplesFromVouchers(vouchers)
	if _, err := exec.LookPath(converter.Command[0]); err != nil {
		// Not fatal: images and e-invoice XML do not need it.
		log.Warn("converter not found: PDF and Word uploads will fail until it is installed (docs/demo-runbook.md)", "command", converter.Command[0])
	}
	reader := &extract.Client{BaseURL: modelURL, Model: model, TextModel: textModel, DateOrder: order}

	rates := loadRates(log, env("DEMO_FX", filepath.Join(data, "fx-rates.json")))

	handler := demo.New(demo.Config{
		History:   history,
		FX:        rates,
		Extractor: &ingest.Reader{Vision: reader, Text: reader, Convert: converter},
		Samples:   os.DirFS(filepath.Join(data, "invoices")),
		MaxUpload: 8 << 20,
		User:      user,
		Pass:      pass,
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       demo.ReadTimeout,
		WriteTimeout:      demo.WriteTimeout, // covers the read plus the model deadline; see demo.DefaultModelTimeout
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("demo listening", "addr", addr, "model", model, "model_url", modelURL,
		"auth", user != "", "past_bookings", len(history), "date_order", map[extract.DateOrder]string{extract.DMY: "dmy", extract.MDY: "mdy"}[order])
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
