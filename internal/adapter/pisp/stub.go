// Package pisp provides implementations of domain.PaymentSubmitter.
// The Stub is a no-op implementation used until a real bank API partner is
// selected. Replace with adapter/tink or adapter/nets when the PISP contract
// is signed.
package pisp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// Stub implements domain.PaymentSubmitter without calling any bank API.
// It logs the submission, assigns a fake reference, and returns immediately.
// The returned SubmissionRef encodes the timestamp so it's unique per call.
type Stub struct {
	log *slog.Logger
}

// NewStub returns a Stub PaymentSubmitter.
func NewStub(log *slog.Logger) *Stub { return &Stub{log: log} }

// Submit implements domain.PaymentSubmitter.
func (s *Stub) Submit(_ context.Context, b domain.Batch, account domain.DebtorAccount) (domain.SubmissionRef, error) {
	ref := domain.SubmissionRef(fmt.Sprintf("STUB-%s-%s", b.MsgID, time.Now().UTC().Format("150405")))
	s.log.Info("pisp stub: batch submitted",
		"batch_id", b.ID,
		"msg_id", b.MsgID,
		"items", len(b.Items),
		"debtor_iban", account.IBAN,
		"ref", ref,
	)
	return ref, nil
}
