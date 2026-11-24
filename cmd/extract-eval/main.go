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
	"errors"
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
	dateOrder := flag.String("date-order", "dmy", "how 02.11.2026 is read when both readings are valid: dmy or mdy")
	flag.Parse()

	order := extract.DMY
	switch *dateOrder {
	case "dmy":
	case "mdy":
		order = extract.MDY
	default:
		fmt.Fprintln(os.Stderr, "extract-eval: -date-order must be dmy or mdy")
		os.Exit(2)
	}
	if err := run(*dir, *model, *url, *limit, order); err != nil {
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

func run(dir, model, url string, limit int, order extract.DateOrder) error {
	var truth []extract.Truth
	if err := readJSON(filepath.Join(dir, "truth.json"), &truth); err != nil {
		return err
	}
	// history.json is optional: public datasets carry no booking history, and
	// then the proposer stages are skipped rather than reported as zero.
	var vouchers []domain.Voucher
	hasHistory := true
	if err := readJSON(filepath.Join(dir, "history.json"), &vouchers); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		hasHistory = false
	}
	if limit > 0 && limit < len(truth) {
		truth = truth[:limit]
	}

	history := booking.ExamplesFromVouchers(vouchers)
	proposer := booking.NewProposer(history)
	if hasHistory {
		held := booking.Evaluate(history, 0.2)
		fmt.Printf("HISTORY  %d past bookings. Held-out (latest 20%% by date): top-1 %s, top-3 %s, unseen suppliers %d\n\n",
			len(history), pct(held.Top1, held.Test), pct(held.Top3, held.Test), held.Unseen)
	} else {
		fmt.Printf("HISTORY  none in %s: booking stages skipped\n\n", dir)
	}

	client := &extract.Client{BaseURL: url, Model: model, DateOrder: order}
	fieldRight := map[string]int{}
	fieldLabelled := map[string]int{}
	var read, fieldsRight, fieldsTotal, e2eTop1, e2eTop3, e2eUnseen, e2eN int
	var elapsed time.Duration
	var totalWrong, totalWrongCaught, totalRightFlagged, totalRight int
	var dateAmbiguous, dateAmbiguousWrong, dateClearWrong, dateClear int

	for _, tr := range truth {
		img, err := os.ReadFile(filepath.Join(dir, tr.File))
		if err != nil {
			// .gitignore excludes *.png, so images are never in the repo: the
			// synthetic set is regenerated, and public sets are prepared.
			return fmt.Errorf("read image %s: %w (synthetic: uv run scripts/gen-synthetic-invoices.py; public: uv run scripts/prep-dataset.py)", tr.File, err)
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
		for _, f := range s.RightFields {
			fieldRight[f]++
			fieldLabelled[f]++
		}
		for _, f := range s.Wrong {
			fieldLabelled[f]++
		}

		// Does the net + VAT = total check earn its keep, and are ambiguous
		// dates the ones that go wrong?
		if tr.Total != "" {
			if contains(s.Wrong, "total") {
				totalWrong++
				if !inv.Reconciled() {
					totalWrongCaught++
				}
			} else {
				totalRight++
				if !inv.Reconciled() {
					totalRightFlagged++
				}
			}
		}
		if tr.InvoiceDate != "" {
			wrongDate := contains(s.Wrong, "invoice_date")
			if inv.InvoiceDateAmbiguous {
				dateAmbiguous++
				if wrongDate {
					dateAmbiguousWrong++
				}
			} else {
				dateClear++
				if wrongDate {
					dateClearWrong++
				}
			}
		}

		verdict := ""
		got := booking.Proposal{}
		if hasHistory {
			got = proposer.Propose(inv.Supplier)
			e2eN++
			verdict = "unseen"
		}
		switch {
		case !hasHistory:
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
		fmt.Printf("  %-15s %s\n", f, pct(fieldRight[f], fieldLabelled[f]))
	}
	fmt.Printf("  all fields      %s\n", pct(fieldsRight, fieldsTotal))
	fmt.Printf("\nTOTAL CHECK (net + VAT = total, within 0.50)\n")
	fmt.Printf("  wrong totals caught as unreconciled: %s\n", pct(totalWrongCaught, totalWrong))
	fmt.Printf("  correct totals wrongly flagged:      %s\n", pct(totalRightFlagged, totalRight))
	fmt.Printf("\nDATE CHECK (order %s)\n", map[extract.DateOrder]string{extract.DMY: "dmy", extract.MDY: "mdy"}[order])
	fmt.Printf("  flagged ambiguous: %d, of which wrong: %s\n", dateAmbiguous, pct(dateAmbiguousWrong, dateAmbiguous))
	fmt.Printf("  not ambiguous:     %d, of which wrong: %s\n", dateClear, pct(dateClearWrong, dateClear))
	if hasHistory {
		fmt.Printf("\nEND TO END  image -> read supplier -> proposed account vs the account booked\n")
		fmt.Printf("  top-1 %s, top-3 %s, unseen suppliers %d\n", pct(e2eTop1, e2eN), pct(e2eTop3, e2eN), e2eUnseen)
	}
	fmt.Println("\nThese numbers describe this dataset only. They are not a claim about any real company's books.")
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
