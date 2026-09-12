package main

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/crypto"
)

// Fortnox rotates the refresh token on every use, so "did it rotate" is the
// only signal that a write-back is needed. Getting this wrong is expensive in
// both directions: a false negative loses the rotated token and forces a
// re-auth (#37); a false positive writes an identical row and, worse, would
// clobber a token the running pod may have rotated in the meantime.
func TestShouldWriteBack(t *testing.T) {
	tests := []struct {
		name       string
		oldRefresh string
		newRefresh string
		want       bool
	}{
		{"rotated", "old-rt", "new-rt", true},
		{"unchanged", "same-rt", "same-rt", false},
		{"unchanged despite trailing newline from the file read", "same-rt\n", "same-rt", false},
		{"unchanged despite surrounding whitespace", "  same-rt  ", "same-rt", false},
		{"empty new token is never written back", "old-rt", "", false},
		{"whitespace-only new token is never written back", "old-rt", "   \n", false},
		{"first run has no prior token", "", "new-rt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldWriteBack(tt.oldRefresh, tt.newRefresh); got != tt.want {
				t.Errorf("shouldWriteBack(%q, %q) = %v, want %v",
					tt.oldRefresh, tt.newRefresh, got, tt.want)
			}
		})
	}
}

// The compare-and-set is the part that can lose a token, and it had no test at
// all — the commit that shipped it claimed "unit-tested CAS logic" on the
// strength of TestShouldWriteBack above, which only compares strings
// (cobalt-dingo#41).

type fakeExecer struct {
	rows    int64
	err     error
	gotArgs []any
	calls   int
}

func (f *fakeExecer) Exec(_ string, args ...any) (sql.Result, error) {
	f.calls++
	f.gotArgs = args
	if f.err != nil {
		return nil, f.err
	}
	return fakeResult{rows: f.rows}, nil
}

type fakeResult struct{ rows int64 }

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.rows, nil }

// testCipherSecret is high-entropy test-only material, shaped like the
// 1Password recipe that provisions the real key.
const testCipherSecret = "Kq7#vP2mZx9!tR4wLs8@nB6yHj3^dF5gCe1%uA0oXi7*qW2zTv4rMk9$bN6pJ8hY"

func TestWriteBack_AppliesRotationUnderCAS(t *testing.T) {
	t.Setenv("FORTNOX_INTEGRATION_KEY", testCipherSecret)
	f := &fakeExecer{rows: 1}
	tok := token{AccessToken: "new-at", RefreshToken: "new-rt", ExpiresAt: "2026-08-19T10:00:00Z"}

	if err := writeBack(f, tok, "old-rt"); err != nil {
		t.Fatalf("writeBack: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("calls = %d, want 1", f.calls)
	}

	// The CAS condition must be the FINGERPRINT of the prior refresh token.
	//
	// This is the property #85 turns on. AES-GCM is randomised, so a CAS that
	// compared sealed values would never match: the write-back would no-op
	// every run, print "the pod won", and lose the rotated token — a silent
	// failure indistinguishable from success, which is exactly what the
	// plaintext CAS was protecting against.
	cipher, err := crypto.NewCipher(testCipherSecret)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	last := f.gotArgs[len(f.gotArgs)-1]
	if last != cipher.Fingerprint("old-rt") {
		t.Fatalf("CAS argument = %v, want the fingerprint of the pre-run refresh token", last)
	}
	if last == "old-rt" {
		t.Fatal("CAS argument is the plaintext refresh token — the column no longer holds it")
	}
}

// Nothing written to the database may be plaintext. A sealed column holding a
// readable token would be undetectable by inspection.
func TestWriteBack_storesNoPlaintext(t *testing.T) {
	t.Setenv("FORTNOX_INTEGRATION_KEY", testCipherSecret)
	f := &fakeExecer{rows: 1}

	if err := writeBack(f, token{AccessToken: "plain-at", RefreshToken: "plain-rt", ExpiresAt: "2026-08-19T10:00:00Z"}, "old-rt"); err != nil {
		t.Fatalf("writeBack: %v", err)
	}

	for i, a := range f.gotArgs {
		got, ok := a.(string)
		if !ok {
			continue
		}
		if strings.Contains(got, "plain-at") || strings.Contains(got, "plain-rt") || got == "old-rt" {
			t.Fatalf("arg %d carries a plaintext token: %q", i, got)
		}
	}
}

// Without the key this command must refuse rather than fall back to writing
// plaintext the running application could not decrypt.
func TestWriteBack_refusesWithoutTheKey(t *testing.T) {
	t.Setenv("FORTNOX_INTEGRATION_KEY", "")
	f := &fakeExecer{rows: 1}

	err := writeBack(f, token{AccessToken: "at", RefreshToken: "rt"}, "old-rt")

	if err == nil {
		t.Fatal("writeBack must refuse without FORTNOX_INTEGRATION_KEY")
	}
	if f.calls != 0 {
		t.Fatal("nothing may be written without the key")
	}
}

// A concurrent refresh by the running pod moves the row, so the CAS matches
// nothing. That is a correct outcome, not a failure — the newer token stands.
func TestWriteBack_ZeroRowsIsNotAnError(t *testing.T) {
	t.Setenv("FORTNOX_INTEGRATION_KEY", testCipherSecret)
	f := &fakeExecer{rows: 0}
	err := writeBack(f, token{RefreshToken: "new-rt"}, "old-rt")
	if err != nil {
		t.Fatalf("a lost CAS race must not be an error: %v", err)
	}
}

// A failed write-back means the rotated token is gone and the next run needs a
// manual re-auth. It must NOT exit zero: a green CI run is exactly how this
// stays invisible until it bites.
func TestWriteBack_DatabaseErrorIsReturned(t *testing.T) {
	t.Setenv("FORTNOX_INTEGRATION_KEY", testCipherSecret)
	f := &fakeExecer{err: errors.New("connection reset")}
	err := writeBack(f, token{RefreshToken: "new-rt"}, "old-rt")
	if err == nil {
		t.Fatal("a failed write-back must surface, not be swallowed")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("error should carry the cause, got: %v", err)
	}
}

// An empty prior refresh token binds "" as the CAS condition, matches no row,
// and reports the benign "concurrent refresh" outcome — so the command exits 0
// and CI goes green while the rotated token is gone. Same silent loss #41 was
// filed to remove, one branch over.
func TestWriteBack_EmptyPriorTokenIsRefused(t *testing.T) {
	f := &fakeExecer{rows: 0}
	err := writeBack(f, token{RefreshToken: "new-rt"}, "")
	if err == nil {
		t.Fatal("an empty CAS condition must be refused, not reported as a lost race")
	}
	if f.calls != 0 {
		t.Fatalf("no UPDATE should be attempted, got %d", f.calls)
	}
}
