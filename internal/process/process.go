// Package process turns one invoice image into a reviewed-able proposal: read
// the fields, propose a booking from past bookings, and say how far to trust
// it. It reads and proposes; it never writes to any ledger.
package process

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
)

// Extractor reads an invoice image. *extract.Client implements it.
type Extractor interface {
	Extract(ctx context.Context, image []byte) (extract.Invoice, error)
}

// Result is what a person reviews.
type Result struct {
	Invoice  extract.Invoice
	Proposal booking.Proposal

	// Warnings are what to check before relying on this result.
	Warnings []string

	// DoNotTrust is true when the invoice's amounts do not reconcile. The
	// proposal is still returned so a person can see what was read, but it
	// must be shown marked, never as a clean proposal.
	DoNotTrust bool
}

// Run reads the image and proposes a booking. An extraction error is returned
// as an error; there is no proposal built on a reading that did not parse.
func Run(ctx context.Context, image []byte, ex Extractor, p *booking.Proposer) (Result, error) {
	inv, err := ex.Extract(ctx, image)
	if err != nil {
		return Result{}, fmt.Errorf("read invoice: %w", err)
	}
	return Result{
		Invoice:    inv,
		Proposal:   p.Propose(inv.Supplier),
		Warnings:   inv.Warnings(),
		DoNotTrust: !inv.Reconciled(),
	}, nil
}
