package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
)

// UserDirectory resolves a verified email to the internal user ID that
// credentials are keyed by (ADR-0003), minting one on first login.
type UserDirectory struct {
	s      *Store
	issuer string
}

// NewUserDirectory returns a UserDirectory backed by s. issuer is recorded
// alongside each subject, so that after an IdP change the audit trail says
// which provider issued which subject — the question that was unanswerable
// when the Dex→Authentik migration orphaned a token.
func NewUserDirectory(s *Store, issuer string) *UserDirectory {
	return &UserDirectory{s: s, issuer: issuer}
}

// ResolveOwner implements auth.OwnerDirectory.
//
// Insert-then-read rather than read-then-insert: two concurrent first logins
// would both see no row and both insert. `ON CONFLICT (email) DO NOTHING`
// makes the loser a no-op, and the following read returns whichever id won —
// so both sessions agree, which is what matters. A read-then-insert would
// produce two ids for one human and split their credentials.
func (d *UserDirectory) ResolveOwner(ctx context.Context, email, sub string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", errors.New("cannot resolve an owner without an email address")
	}

	id, err := newUserID()
	if err != nil {
		return "", err
	}
	if err := d.s.queries.InsertUser(ctx, pgstore.InsertUserParams{ID: id, Email: email}); err != nil {
		return "", fmt.Errorf("mint user: %w", err)
	}

	row, err := d.s.queries.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		// Cannot happen after a successful insert, and worth failing loudly if
		// it somehow does: returning "" here would key credentials on nothing.
		return "", fmt.Errorf("user %q vanished between insert and read", email)
	}
	if err != nil {
		return "", fmt.Errorf("read user: %w", err)
	}

	// Audit only, and deliberately non-fatal: a failure to record which
	// subject was presented must not block a login, because the subject is not
	// load-bearing any more. That is the whole point of ADR-0003.
	if err := d.s.queries.RecordUserSubject(ctx, pgstore.RecordUserSubjectParams{
		UserID: row.ID, Sub: sub, Issuer: d.issuer,
	}); err != nil {
		return row.ID, nil //nolint:nilerr // audit write, never a login blocker
	}
	return row.ID, nil
}

// newUserID returns an opaque, application-owned identifier.
//
// Deliberately not derived from the email or the subject: a derived id inherits
// whatever instability its input has, which is the defect being fixed. Random
// means an email change is a one-row update rather than a re-key.
func newUserID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate user id: %w", err)
	}
	return "u_" + hex.EncodeToString(b), nil
}
