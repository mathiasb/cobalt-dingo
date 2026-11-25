// Command extract-eval measures the invoice processor on the synthetic set:
// how well the vision model reads invoice fields, how well the booking
// proposer ranks the right account from past bookings, and the two combined.
//
//	go run ./cmd/extract-eval [-dir testdata/synthetic] [-model qwen2.5vl:7b]
//	                          [-text-model M] [-url http://iguana:11434] [-limit N]
//	                          [-formats pdf_text,docx,ubl]
//
// A directory may mix formats (scripts/gen-variety.py): each file is read the way
// its kind is read in the demo, and the report is also broken down per format.
// Scans are expected to be refused, not read, and are counted as refused.
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
	"sort"
	"strings"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/ingest"
)

func main() {
	dir := flag.String("dir", "testdata/synthetic", "synthetic data directory")
	model := flag.String("model", "qwen2.5vl:7b", "vision model name")
	url := flag.String("url", "http://iguana:11434", "Ollama-compatible base URL (local network only)")
	limit := flag.Int("limit", 0, "evaluate only the first N images (0 = all)")
	textModel := flag.String("text-model", "", "model that reads text from PDF and Word files (default: -model)")
	formats := flag.String("formats", "", "comma-separated formats to evaluate (default: all in truth.json)")
	markitdown := flag.String("markitdown", "", "converter command for PDF and Word (default: uvx markitdown)")
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
	if err := run(*dir, *model, *textModel, *url, *limit, order, *formats, *markitdown); err != nil {
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

func run(dir, model, textModel, url string, limit int, order extract.DateOrder, formats, markitdown string) error {
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
	if formats != "" {
		keep := map[string]bool{}
		for _, f := range strings.Split(formats, ",") {
			keep[strings.TrimSpace(f)] = true
		}
		var kept []extract.Truth
		for _, tr := range truth {
			if keep[tr.Format] {
				kept = append(kept, tr)
			}
		}
		truth = kept
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

	mc := &extract.Client{BaseURL: url, Model: model, TextModel: textModel, DateOrder: order}
	conv := ingest.NewMarkitdown()
	if markitdown != "" {
		conv.Command = strings.Fields(markitdown)
	}
	client := &ingest.Reader{Vision: mc, Text: mc, Convert: conv}
	byFormat := map[string]*formatTally{}
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
		took := time.Since(start)
		elapsed += took
		ft := tally(byFormat, tr.Format)
		ft.n++
		ft.elapsed += took
		if err != nil {
			if errors.Is(err, ingest.ErrScanned) || errors.Is(err, ingest.ErrRefused) {
				ft.refused++
				fmt.Printf("%-18s REFUSED: %v\n", tr.File, err)
				continue
			}
			fmt.Printf("%-18s EXTRACT FAILED: %v\n", tr.File, err)
			continue
		}
		read++
		ft.read++
		s := extract.Score(tr, inv)
		ft.fieldsRight += s.Right
		ft.fieldsTotal += s.Fields
		if inv.Reconciled() {
			ft.reconciled++
		}
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

	if read == 0 && !onlyRefusals(byFormat) {
		return fmt.Errorf("no document could be read by %s at %s: not reporting a result", model, url)
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
	if len(byFormat) > 1 || (len(byFormat) == 1 && byFormat[""] == nil) {
		fmt.Printf("\nBY FORMAT  (read = produced an invoice; refused = scan or malformed, said so and gave no entry)\n")
		fmt.Printf("  %-10s %5s %5s %8s %-22s %-18s %s\n", "format", "files", "read", "refused", "fields right", "reconciled", "mean time")
		names := make([]string, 0, len(byFormat))
		for k := range byFormat {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			ft := byFormat[k]
			name := k
			if name == "" {
				name = "(image)"
			}
			fmt.Printf("  %-10s %5d %5d %8d %-22s %-18s %.1fs\n", name, ft.n, ft.read, ft.refused, pct(ft.fieldsRight, ft.fieldsTotal), pct(ft.reconciled, ft.read), ft.elapsed.Seconds()/float64(max(ft.n, 1)))
		}
	}
	fmt.Println("\nThese numbers describe this dataset only. They are not a claim about any real company's books.")
	return nil
}

// formatTally is the per-format row of the report.
type formatTally struct {
	n, read, refused, fieldsRight, fieldsTotal, reconciled int
	elapsed                                                time.Duration
}

func tally(m map[string]*formatTally, k string) *formatTally {
	if m[k] == nil {
		m[k] = &formatTally{}
	}
	return m[k]
}

// onlyRefusals reports whether every file was refused: a set of scans alone is a
// legitimate run, an unreachable model is not.
func onlyRefusals(m map[string]*formatTally) bool {
	if len(m) == 0 {
		return false
	}
	for _, ft := range m {
		if ft.refused != ft.n {
			return false
		}
	}
	return true
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
