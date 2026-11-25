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
//	DEMO_TEXT_MODEL  model that reads text from PDF and Word files (default: DEMO_MODEL)
//	DEMO_MARKITDOWN  converter command for PDF and Word (default: uvx --quiet --from markitdown[docx,pdf] markitdown)
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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/demo"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/ingest"
)

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
	reader := &extract.Client{BaseURL: modelURL, Model: model, TextModel: textModel, DateOrder: order}

	handler := demo.New(demo.Config{
		History:   history,
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
