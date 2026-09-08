// Package statements ingests bank and card statements from the files their
// providers produce, and normalises them into transactions.
//
// It exists because Fortnox's API exposes no bank-transaction endpoint — only
// booked material — so a reconciliation that reads Fortnox alone is checking
// Fortnox against itself. See docs/adr/0002-ingest-files-not-vendor-integrations.md.
//
// The acquisition layer is deliberately disposable: a file downloaded by hand,
// forwarded by mail, or one day fetched from an aggregator API. What is *not*
// disposable is the model that file lands in — the Transaction shape, the
// idempotency key and the provenance fields below are what let the acquisition
// layer be replaced without a migration.
package statements

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// Source records where a transaction was acquired from. It is provenance only
// and never participates in identity — see Transaction.IdempotencyKey.
type Source string

const (
	// SourceCamt053 is an ISO 20022 camt.053 bank statement.
	SourceCamt053 Source = "camt053"
	// SourceCSV is a provider CSV export (card statements, broker exports).
	SourceCSV Source = "csv"
	// SourceAggregator is a licensed account-information provider. Not
	// implemented; declared so the identity test can prove that switching to
	// one does not re-key existing transactions.
	SourceAggregator Source = "aggregator"
)

// Direction is the sign of a transaction from the account holder's view.
type Direction string

const (
	// Credit is money into the account (camt CdtDbtInd=CRDT).
	Credit Direction = "credit"
	// Debit is money out of the account (camt CdtDbtInd=DBIT).
	Debit Direction = "debit"
)

// Parser turns one provider's statement file into a Statement. Every source
// without a usable API implements this; an aggregator would implement it too,
// returning the same type, so nothing downstream learns where data came from.
type Parser interface {
	Parse(r io.Reader, sourceRef string) (Statement, error)
}

// Transaction is one booked movement on an account, shaped on camt.053's field
// set rather than on any particular CSV. Every aggregator emits something
// mappable onto ISO 20022; shaping this on a provider's CSV export would
// silently make that CSV the schema.
type Transaction struct {
	Account      string       // IBAN or BBAN of the account the movement is on
	BookingDate  time.Time    // when the bank booked it
	ValueDate    time.Time    // when value transferred; may differ from booking
	Amount       domain.Money // always positive; Direction carries the sign
	Direction    Direction
	BankRef      string // AcctSvcrRef — the bank's own reference for the entry
	EndToEndID   string // payment-initiation reference, when present
	Counterparty string
	Remittance   string

	// Provenance. Never part of identity.
	Source    Source
	SourceRef string // the file or export this came from
}

// SignedMinorUnits returns the amount with the direction applied: negative for
// a debit. Callers reconciling against a balance want this, not Amount.
func (t Transaction) SignedMinorUnits() int64 {
	if t.Direction == Debit {
		return -t.Amount.MinorUnits
	}
	return t.Amount.MinorUnits
}

// IdempotencyKey identifies a transaction independently of how it was acquired.
//
// Both a manual export and an aggregator re-deliver overlapping date ranges, so
// re-ingesting must not create new rows — double-counted money is the failure
// that destroys trust in the whole reconciliation. Source and SourceRef are
// deliberately excluded: the same transaction delivered by a different route is
// still the same transaction, which is what makes switching sources safe.
func (t Transaction) IdempotencyKey() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d|%s|%s",
		t.Account,
		t.BookingDate.UTC().Format("2006-01-02"),
		t.SignedMinorUnits(),
		t.Amount.Currency,
		t.BankRef,
	)
	return hex.EncodeToString(h.Sum(nil))
}

// Statement is one account's booked activity over a period, with the balances
// that bracket it.
type Statement struct {
	ID             string
	Account        string
	From, To       time.Time
	OpeningBalance domain.Money
	ClosingBalance domain.Money
	Transactions   []Transaction
	Source         Source
	SourceRef      string
}

// CheckBalance asserts the identity that makes a parse trustworthy: opening
// balance plus the net of every booked transaction equals the closing balance.
//
// This is the acceptance criterion for statement ingestion. A parser that drops
// or duplicates a row still returns entirely plausible transactions — nothing
// but this identity notices.
func (s Statement) CheckBalance() error {
	var net int64
	for _, t := range s.Transactions {
		net += t.SignedMinorUnits()
	}

	want := s.ClosingBalance.MinorUnits
	got := s.OpeningBalance.MinorUnits + net
	if got != want {
		return fmt.Errorf(
			"statement %s does not reconcile: opening %d + net %d = %d, but closing balance is %d (off by %d minor units across %d transactions)",
			s.ID, s.OpeningBalance.MinorUnits, net, got, want, got-want, len(s.Transactions))
	}
	return nil
}

// parseDecimalMinor converts an ISO 20022 decimal string ("325.50") to minor
// units, exactly.
//
// Deliberately not via float64: 325.50 has no exact binary representation, and
// a cent lost per row is invisible until a balance check fails months later.
// domain.Money.FromFloat is fine for human input; it is not fine here.
func parseDecimalMinor(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}

	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")

	whole, frac, _ := strings.Cut(s, ".")

	// Pad or reject: two decimal places is the currency exponent we store.
	switch {
	case len(frac) == 0:
		frac = "00"
	case len(frac) == 1:
		frac += "0"
	case len(frac) > 2:
		// Trailing zeros beyond two places are harmless; real precision is not.
		if strings.Trim(frac[2:], "0") != "" {
			return 0, fmt.Errorf("amount %q has more precision than minor units can hold", s)
		}
		frac = frac[:2]
	}

	var minor int64
	if _, err := fmt.Sscanf(whole+frac, "%d", &minor); err != nil {
		return 0, fmt.Errorf("parse amount %q: %w", s, err)
	}
	if neg {
		minor = -minor
	}
	return minor, nil
}
