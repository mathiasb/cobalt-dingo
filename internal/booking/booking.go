// Package booking proposes how a supplier invoice should be booked, learned
// from a customer's own past bookings. It reads history and returns proposals;
// it never writes to any ledger.
//
// Method: retrieval, not training. A new supplier text is matched against the
// supplier texts of past bookings, and the accounts those bookings used are
// ranked by similarity-weighted votes. Every proposal carries the past
// bookings it rests on (Because), so a reviewer can check the reasoning.
package booking

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

const (
	supplierLedgerAccount = 2440
	vatFirst, vatLast     = 2640, 2649
	expenseFirst          = 4000
	expenseLast           = 8999

	// matchThreshold is the minimum token overlap for a past booking to count
	// as the same supplier. Below it a supplier is treated as unseen.
	matchThreshold = 0.5
)

// Example is one past supplier-invoice booking.
type Example struct {
	Supplier   string
	Account    int // expense account
	VATAccount int // input VAT account, 0 if none
	CostCenter string
	Series     string
	Number     int
	Date       string // YYYY-MM-DD
}

// ExamplesFromVouchers derives examples from journal entries. A voucher counts
// as a supplier-invoice booking when it credits the supplier ledger (2440) and
// debits at least one expense account (4000-8999).
func ExamplesFromVouchers(vs []domain.Voucher) []Example {
	var out []Example
	for _, v := range vs {
		if ex, ok := exampleFrom(v); ok {
			out = append(out, ex)
		}
	}
	return out
}

func exampleFrom(v domain.Voucher) (Example, bool) {
	creditsSupplierLedger := false
	var expense *domain.VoucherRow
	vat := 0
	for i := range v.Rows {
		r := v.Rows[i]
		switch {
		case r.Account == supplierLedgerAccount && r.Credit.MinorUnits > 0:
			creditsSupplierLedger = true
		case r.Account >= vatFirst && r.Account <= vatLast && r.Debit.MinorUnits > 0:
			if vat == 0 {
				vat = r.Account
			}
		case r.Account >= expenseFirst && r.Account <= expenseLast && r.Debit.MinorUnits > 0:
			if expense == nil || r.Debit.MinorUnits > expense.Debit.MinorUnits {
				expense = &v.Rows[i]
			}
		}
	}
	if !creditsSupplierLedger || expense == nil {
		return Example{}, false
	}
	return Example{
		Supplier:   strings.TrimSpace(v.Description),
		Account:    expense.Account,
		VATAccount: vat,
		CostCenter: expense.CostCenter,
		Series:     v.Series,
		Number:     v.Number,
		Date:       v.TransactionDate,
	}, true
}

// Candidate is one proposed expense account.
type Candidate struct {
	Account int
	Score   float64 // share of the similarity-weighted votes, 0..1
}

// Proposal is the answer for one supplier text.
type Proposal struct {
	Candidates []Candidate // best first; empty when the supplier is unseen
	Confidence float64     // 0 when unseen
	Because    []Example   // past bookings behind the top candidate
	VATAccount int         // most recent VAT account used with the top candidate
}

// Proposer proposes bookings from remembered examples.
type Proposer struct{ examples []Example }

// NewProposer returns a Proposer that remembers examples.
func NewProposer(examples []Example) *Proposer {
	return &Proposer{examples: append([]Example(nil), examples...)}
}

// Learn remembers one more booking, for example a correction made by a person.
func (p *Proposer) Learn(e Example) { p.examples = append(p.examples, e) }

// Propose ranks expense accounts for a supplier text.
func (p *Proposer) Propose(supplier string) Proposal {
	want := tokens(supplier)
	if len(want) == 0 {
		return Proposal{}
	}

	weight := map[int]float64{}
	var total, best float64
	type hit struct {
		ex  Example
		sim float64
	}
	var hits []hit
	for _, ex := range p.examples {
		sim := jaccard(want, tokens(ex.Supplier))
		if sim < matchThreshold {
			continue
		}
		weight[ex.Account] += sim
		total += sim
		if sim > best {
			best = sim
		}
		hits = append(hits, hit{ex, sim})
	}
	if total == 0 {
		return Proposal{}
	}

	cands := make([]Candidate, 0, len(weight))
	for acct, w := range weight {
		cands = append(cands, Candidate{Account: acct, Score: w / total})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].Account < cands[j].Account
	})

	top := cands[0].Account
	prop := Proposal{Candidates: cands, Confidence: cands[0].Score * best}
	for _, h := range hits {
		if h.ex.Account == top {
			prop.Because = append(prop.Because, h.ex)
			if h.ex.VATAccount != 0 {
				prop.VATAccount = h.ex.VATAccount
			}
		}
	}
	return prop
}

// Result is the outcome of a held-out evaluation.
type Result struct {
	Test   int // examples in the held-out period
	Unseen int // of those, suppliers with no match in the training period
	Top1   int // right account ranked first
	Top3   int // right account within the first three
}

// Top1Rate is Top1 over all held-out examples; unseen suppliers count as misses.
func (r Result) Top1Rate() float64 { return ratio(r.Top1, r.Test) }

// Top3Rate is Top3 over all held-out examples.
func (r Result) Top3Rate() float64 { return ratio(r.Top3, r.Test) }

// Top1RateSeen is Top1 over only the examples whose supplier was seen in training.
func (r Result) Top1RateSeen() float64 { return ratio(r.Top1, r.Test-r.Unseen) }

func ratio(n, d int) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// Evaluate trains on the earlier part of the history and tests on the latest
// testFraction of it, ordered by date. The split is by time, never random: a
// random split lets the future teach the past and overstates accuracy.
func Evaluate(examples []Example, testFraction float64) Result {
	ex := append([]Example(nil), examples...)
	sort.SliceStable(ex, func(i, j int) bool {
		if ex[i].Date != ex[j].Date {
			return ex[i].Date < ex[j].Date
		}
		if ex[i].Series != ex[j].Series {
			return ex[i].Series < ex[j].Series
		}
		return ex[i].Number < ex[j].Number
	})

	testN := int(math.Round(testFraction * float64(len(ex))))
	if testN < 0 {
		testN = 0
	}
	if testN > len(ex) {
		testN = len(ex)
	}
	p := NewProposer(ex[:len(ex)-testN])

	var res Result
	for _, e := range ex[len(ex)-testN:] {
		res.Test++
		got := p.Propose(e.Supplier)
		if len(got.Candidates) == 0 {
			res.Unseen++
			continue
		}
		for i, c := range got.Candidates {
			if i >= 3 {
				break
			}
			if c.Account == e.Account {
				if i == 0 {
					res.Top1++
				}
				res.Top3++
				break
			}
		}
	}
	return res
}

// noise are words and legal-form suffixes that say nothing about who the
// supplier is. "SAS" is deliberately absent: there it is the name.
var noise = map[string]bool{
	"ab": true, "aktiebolag": true, "gmbh": true, "ltd": true, "inc": true,
	"oy": true, "as": true, "hb": true, "kb": true, "llc": true, "sa": true,
	"invoice": true, "faktura": true, "receipt": true, "kvitto": true,
	"nr": true, "no": true, "the": true,
}

// tokens reduces a supplier text to its identifying words: lower case,
// punctuation removed, words containing digits (invoice numbers) and noise
// words dropped.
func tokens(s string) map[string]bool {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := map[string]bool{}
	for _, w := range words {
		if noise[w] || strings.ContainsFunc(w, unicode.IsDigit) {
			continue
		}
		out[w] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	return float64(inter) / float64(union)
}
