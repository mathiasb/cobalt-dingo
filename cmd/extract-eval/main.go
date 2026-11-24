// Command extract-eval measures the invoice processor on the synthetic set:
// how well the vision model reads invoice fields, how well the booking
// proposer ranks the right account from past bookings, and the two combined.
//
//	go run ./cmd/extract-eval [-dir testdata/synthetic] [-model qwen2.5vl:7b]
//	                          [-url http://iguana:11434] [-limit N]
//
// It reads files and calls a local model. It writes nothing anywhere and
// exits non-zero when no image could be read, so an unreachable model cannot
// look like a pass.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
)

func main() {
	dir := flag.String("dir", "testdata/synthetic", "synthetic data directory")
	model := flag.String("model", "qwen2.5vl:7b", "vision model name")
	url := flag.String("url", "http://iguana:11434", "Ollama-compatible base URL (local network only)")
	limit := flag.Int("limit", 0, "evaluate only the first N images (0 = all)")
	flag.Parse()

	if err := run(*dir, *model, *url, *limit); err != nil {
		fmt.Fprintln(os.Stderr, "extract-eval:", err)
		os.Exit(1)
	}
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func pct(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%% (%d/%d)", 100*float64(n)/float64(d), n, d)
}

func run(dir, model, url string, limit int) error {
	var truth []extract.Truth
	if err := readJSON(filepath.Join(dir, "truth.json"), &truth); err != nil {
		return err
	}
	var vouchers []domain.Voucher
	if err := readJSON(filepath.Join(dir, "history.json"), &vouchers); err != nil {
		return err
	}
	if limit > 0 && limit < len(truth) {
		truth = truth[:limit]
	}

	history := booking.ExamplesFromVouchers(vouchers)
	proposer := booking.NewProposer(history)
	held := booking.Evaluate(history, 0.2)
	fmt.Printf("HISTORY  %d past bookings. Held-out (latest 20%% by date): top-1 %s, top-3 %s, unseen suppliers %d\n\n",
		len(history), pct(held.Top1, held.Test), pct(held.Top3, held.Test), held.Unseen)

	client := &extract.Client{BaseURL: url, Model: model}
	fieldRight := map[string]int{}
	var read, fieldsRight, fieldsTotal, e2eTop1, e2eTop3, e2eUnseen, e2eN int
	var elapsed time.Duration

	for _, tr := range truth {
		img, err := os.ReadFile(filepath.Join(dir, tr.File))
		if err != nil {
			return fmt.Errorf("read image: %w", err)
		}
		start := time.Now()
		inv, err := client.Extract(context.Background(), img)
		elapsed += time.Since(start)
		if err != nil {
			fmt.Printf("%-18s EXTRACT FAILED: %v\n", tr.File, err)
			continue
		}
		read++
		s := extract.Score(tr, inv)
		fieldsRight += s.Right
		fieldsTotal += s.Fields
		wrong := map[string]bool{}
		for _, w := range s.Wrong {
			wrong[w] = true
		}
		for _, f := range []string{"supplier", "invoice_number", "invoice_date", "due_date", "currency", "total", "vat_amount"} {
			if !wrong[f] {
				fieldRight[f]++
			}
		}

		got := proposer.Propose(inv.Supplier)
		e2eN++
		verdict := "unseen"
		switch {
		case len(got.Candidates) == 0:
			e2eUnseen++
		default:
			verdict = fmt.Sprintf("proposed %d (want %d, conf %.2f)", got.Candidates[0].Account, tr.ExpectedAccount, got.Confidence)
			for i, c := range got.Candidates {
				if i >= 3 {
					break
				}
				if c.Account == tr.ExpectedAccount {
					if i == 0 {
						e2eTop1++
					}
					e2eTop3++
					break
				}
			}
		}
		fmt.Printf("%-18s fields %d/%d wrong=%v  %s\n", tr.File, s.Right, s.Fields, s.Wrong, verdict)
	}

	if read == 0 {
		return fmt.Errorf("no image could be read by %s at %s: not reporting a result", model, url)
	}

	fmt.Printf("\nEXTRACTION  model %s, %d/%d images read, mean %.1fs per image\n", model, read, len(truth), elapsed.Seconds()/float64(len(truth)))
	for _, f := range []string{"supplier", "invoice_number", "invoice_date", "due_date", "currency", "total", "vat_amount"} {
		fmt.Printf("  %-15s %s\n", f, pct(fieldRight[f], read))
	}
	fmt.Printf("  all fields      %s\n", pct(fieldsRight, fieldsTotal))
	fmt.Printf("\nEND TO END  image -> read supplier -> proposed account vs the account booked\n")
	fmt.Printf("  top-1 %s, top-3 %s, unseen suppliers %d\n", pct(e2eTop1, e2eN), pct(e2eTop3, e2eN), e2eUnseen)
	fmt.Println("\nDATA IS SYNTHETIC. These numbers are not a claim about any real company's books.")
	return nil
}
