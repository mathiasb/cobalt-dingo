package domain_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// ConfirmExecution's partial-failure aggregation had no test at all (#42).
//
// WHY THAT MATTERED
// It is the last step of the money path: the bank has executed, and this is
// where each payment is recorded against its invoice and written back to the
// ERP. Its contract is that a failure on one invoice does not discard the
// others — every error is collected, the batch is still persisted as confirmed,
// and the caller receives all of them joined.
//
// The failure mode is invisible by construction. Replace
//
//	errors.Join(errs...)   with   errs[0]
//
// and the message still reads "confirm execution partial failure (3 errors)",
// because the count comes from len(errs) and the wrapped payload does not. A
// human reading the log sees an accurate count of errors they cannot see. The
// two remaining write-back failures are silently dropped, and the batch is
// confirmed regardless — so the invoices look paid and the vouchers were never
// written.
//
// These tests assert on errors.Is, not on the string, because the string is
// exactly the part that stays correct when the behaviour breaks.

type stubBatchRepo struct {
	batch     domain.Batch
	saved     []domain.Batch
	saveErr   error
	getErrOut error
}

func (s *stubBatchRepo) Get(_ context.Context, _ domain.TenantID, _ domain.BatchID) (domain.Batch, error) {
	if s.getErrOut != nil {
		return domain.Batch{}, s.getErrOut
	}
	return s.batch, nil
}

func (s *stubBatchRepo) Save(_ context.Context, b domain.Batch) error {
	s.saved = append(s.saved, b)
	return s.saveErr
}

func (s *stubBatchRepo) List(_ context.Context, _ domain.TenantID) ([]domain.Batch, error) {
	return nil, nil
}

// failingERP fails for every invoice number in fail, succeeds otherwise.
type failingERP struct {
	fail  map[int]error
	calls []int
}

func (e *failingERP) RecordAndBookkeep(_ context.Context, _ domain.TenantID, item domain.BatchItem, _ float64, _ string) error {
	e.calls = append(e.calls, item.FortnoxInvoiceNumber)
	return e.fail[item.FortnoxInvoiceNumber]
}

func submittedBatch(invoiceNumbers ...int) domain.Batch {
	items := make([]domain.BatchItem, 0, len(invoiceNumbers))
	for _, n := range invoiceNumbers {
		items = append(items, domain.BatchItem{FortnoxInvoiceNumber: n})
	}
	return domain.Batch{
		ID:       domain.BatchID("b-1"),
		TenantID: domain.TenantID("t-1"),
		Status:   domain.BatchStatusSubmitted,
		Items:    items,
	}
}

func confirmations(invoiceNumbers ...int) []domain.ExecutionConfirmation {
	out := make([]domain.ExecutionConfirmation, 0, len(invoiceNumbers))
	for _, n := range invoiceNumbers {
		out = append(out, domain.ExecutionConfirmation{
			FortnoxInvoiceNumber: n,
			ExecutionRate:        11.5,
			PaymentDate:          "2026-09-17",
		})
	}
	return out
}

// Every collected error must be reachable through the returned one. This is the
// assertion that goes red on errs[0].
func TestConfirmExecution_JoinsEveryError(t *testing.T) {
	errA := errors.New("erp down for 101")
	errB := errors.New("erp down for 102")
	errC := errors.New("erp down for 103")

	repo := &stubBatchRepo{batch: submittedBatch(101, 102, 103)}
	erp := &failingERP{fail: map[int]error{101: errA, 102: errB, 103: errC}}
	svc := domain.NewBatchService(repo, nil, nil, erp)

	err := svc.ConfirmExecution(context.Background(), "t-1", "b-1", confirmations(101, 102, 103))
	if err == nil {
		t.Fatal("expected a partial-failure error, got nil")
	}

	for name, want := range map[string]error{"errA": errA, "errB": errB, "errC": errC} {
		if !errors.Is(err, want) {
			t.Errorf("errors.Is could not find %s in the returned error — a caller cannot distinguish "+
				"which invoices failed write-back, and the message still claims the right count: %v", name, err)
		}
	}
}

// A failure on one invoice must not stop the others being attempted. Losing
// this turns one ERP hiccup into a batch-wide write-back outage.
func TestConfirmExecution_ContinuesAfterOneFailure(t *testing.T) {
	repo := &stubBatchRepo{batch: submittedBatch(201, 202, 203)}
	erp := &failingERP{fail: map[int]error{201: errors.New("boom")}}
	svc := domain.NewBatchService(repo, nil, nil, erp)

	_ = svc.ConfirmExecution(context.Background(), "t-1", "b-1", confirmations(201, 202, 203))

	if len(erp.calls) != 3 {
		t.Fatalf("write-back attempted for %v, want all three invoices — a failure on the first "+
			"must not abandon the rest of the batch", erp.calls)
	}
}

// The batch is persisted as confirmed even when write-back fails: the bank has
// already executed, so the batch's status is a fact, not a consequence.
func TestConfirmExecution_PersistsConfirmedDespiteFailures(t *testing.T) {
	repo := &stubBatchRepo{batch: submittedBatch(301)}
	erp := &failingERP{fail: map[int]error{301: errors.New("boom")}}
	svc := domain.NewBatchService(repo, nil, nil, erp)

	if err := svc.ConfirmExecution(context.Background(), "t-1", "b-1", confirmations(301)); err == nil {
		t.Fatal("expected an error")
	}
	if len(repo.saved) != 1 {
		t.Fatalf("batch saved %d times, want 1 — the bank executed, so the batch is confirmed "+
			"whether or not the ERP write-back succeeded", len(repo.saved))
	}
	if repo.saved[0].Status != domain.BatchStatusConfirmed {
		t.Errorf("saved status = %q, want %q", repo.saved[0].Status, domain.BatchStatusConfirmed)
	}
}

// A save failure is itself a collected error, and must be joined alongside the
// write-back failures rather than replacing or hiding them.
func TestConfirmExecution_JoinsSaveErrorWithWriteBackErrors(t *testing.T) {
	erpErr := errors.New("erp down")
	saveErr := errors.New("postgres gone")

	repo := &stubBatchRepo{batch: submittedBatch(401), saveErr: saveErr}
	erp := &failingERP{fail: map[int]error{401: erpErr}}
	svc := domain.NewBatchService(repo, nil, nil, erp)

	err := svc.ConfirmExecution(context.Background(), "t-1", "b-1", confirmations(401))
	if !errors.Is(err, erpErr) {
		t.Errorf("the write-back error was lost: %v", err)
	}
	if !errors.Is(err, saveErr) {
		t.Errorf("the save error was lost — the batch may not be persisted at all and the caller "+
			"cannot tell: %v", err)
	}
}

// An invoice number that is not in the batch is a caller error, collected like
// any other rather than aborting the confirmations that are valid.
func TestConfirmExecution_UnknownInvoiceIsCollectedNotFatal(t *testing.T) {
	repo := &stubBatchRepo{batch: submittedBatch(501)}
	erp := &failingERP{fail: map[int]error{}}
	svc := domain.NewBatchService(repo, nil, nil, erp)

	err := svc.ConfirmExecution(context.Background(), "t-1", "b-1", confirmations(501, 999))
	if err == nil {
		t.Fatal("expected an error naming the unknown invoice")
	}
	if len(erp.calls) != 1 || erp.calls[0] != 501 {
		t.Errorf("write-back calls = %v, want only [501] — the known invoice must still be "+
			"processed alongside the unknown one", erp.calls)
	}
}

// Guard against the whole suite passing vacuously: if ConfirmExecution ever
// stops returning an error for a batch in the wrong state, every test above
// would still need to be read to notice.
func TestConfirmExecution_RejectsNonSubmittedBatch(t *testing.T) {
	b := submittedBatch(601)
	b.Status = domain.BatchStatusConfirmed
	repo := &stubBatchRepo{batch: b}
	svc := domain.NewBatchService(repo, nil, nil, &failingERP{fail: map[int]error{}})

	err := svc.ConfirmExecution(context.Background(), "t-1", "b-1", confirmations(601))
	if err == nil {
		t.Fatal("confirming an already-confirmed batch must fail")
	}
	if len(repo.saved) != 0 {
		t.Errorf("batch was saved %d times for a non-submitted batch, want 0", len(repo.saved))
	}
	_ = fmt.Sprint(err)
}
