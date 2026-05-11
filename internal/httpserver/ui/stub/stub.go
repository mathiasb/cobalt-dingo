// Package stub provides in-memory implementations of the UI interfaces for
// use in tests and during local development before the PostgreSQL backend
// (issue #2) is wired in.
package stub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mathiasb/coo-agent/internal/httpserver/ui/templates"
)

// Tokens implements ui.TokenChecker with configurable state.
type Tokens struct {
	Status templates.TokenStatus
	Err    error
}

// TokenStatus returns the stub token status.
func (t *Tokens) TokenStatus(_ context.Context) (templates.TokenStatus, error) {
	return t.Status, t.Err
}

// RefreshToken simulates a token refresh.
func (t *Tokens) RefreshToken(_ context.Context) error {
	if t.Err != nil {
		return t.Err
	}
	t.Status.ExpiresAt = time.Now().Add(3600 * time.Second)
	return nil
}

// RevokeToken simulates a token revocation.
func (t *Tokens) RevokeToken(_ context.Context) error {
	if t.Err != nil {
		return t.Err
	}
	t.Status = templates.TokenStatus{}
	return nil
}

// ConnectedTokens returns a Tokens stub with an active token expiring in 30 days.
func ConnectedTokens() *Tokens {
	return &Tokens{
		Status: templates.TokenStatus{Connected: true, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)},
	}
}

// DisconnectedTokens returns a Tokens stub with no active token.
func DisconnectedTokens() *Tokens { return &Tokens{} }

// Keys implements ui.APIKeyStore with an in-memory slice.
type Keys struct {
	keys []templates.APIKey
	Err  error
}

// ListKeys returns all in-memory API keys.
func (k *Keys) ListKeys(_ context.Context) ([]templates.APIKey, error) {
	if k.Err != nil {
		return nil, k.Err
	}
	out := make([]templates.APIKey, len(k.keys))
	copy(out, k.keys)
	return out, nil
}

// CreateKey creates a new in-memory API key with a random plaintext token.
func (k *Keys) CreateKey(_ context.Context, label string) (templates.CreatedKey, error) {
	if k.Err != nil {
		return templates.CreatedKey{}, k.Err
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	plain := "coo_stub_" + hex.EncodeToString(b)
	key := templates.APIKey{
		ID:        fmt.Sprintf("stub-%d", len(k.keys)+1),
		Label:     label,
		CreatedAt: time.Now(),
	}
	k.keys = append(k.keys, key)
	return templates.CreatedKey{APIKey: key, Plaintext: plain}, nil
}

// RevokeKey removes an API key by ID from the in-memory store.
func (k *Keys) RevokeKey(_ context.Context, id string) error {
	if k.Err != nil {
		return k.Err
	}
	for i, key := range k.keys {
		if key.ID == id {
			k.keys = append(k.keys[:i], k.keys[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("nyckel %s hittades inte", id)
}

// SeedKeys returns a Keys stub pre-populated with n example keys.
func SeedKeys(n int) *Keys {
	ks := &Keys{}
	for i := range n {
		now := time.Now().Add(-time.Duration(i) * 24 * time.Hour)
		ks.keys = append(ks.keys, templates.APIKey{
			ID:        fmt.Sprintf("stub-%d", i+1),
			Label:     fmt.Sprintf("Exempelnyckel %d", i+1),
			CreatedAt: now,
		})
	}
	return ks
}

// Audit implements ui.AuditReader with a fixed set of fake events.
type Audit struct {
	Events []templates.AuditEvent
	Err    error
}

// ListEvents returns a paginated slice of stub audit events.
func (a *Audit) ListEvents(_ context.Context, page, pageSize int) ([]templates.AuditEvent, error) {
	if a.Err != nil {
		return nil, a.Err
	}
	start := (page - 1) * pageSize
	if start >= len(a.Events) {
		return nil, nil
	}
	end := start + pageSize
	if end > len(a.Events) {
		end = len(a.Events)
	}
	return a.Events[start:end], nil
}

// SeedAudit returns an Audit stub with n fake events.
func SeedAudit(n int) *Audit {
	ops := []string{"GET /invoices", "GET /vouchers", "GET /accounts", "GET /vatreports"}
	evts := make([]templates.AuditEvent, n)
	for i := range n {
		evts[i] = templates.AuditEvent{
			Timestamp:  time.Now().Add(-time.Duration(i) * time.Minute),
			Operation:  ops[i%len(ops)],
			Endpoint:   "/3" + ops[i%len(ops)][3:],
			Method:     "GET",
			StatusCode: 200,
			DurationMS: 40 + i*3,
		}
	}
	return &Audit{Events: evts}
}

// Health implements ui.HealthChecker with configurable flags.
type Health struct {
	DB      bool
	Fortnox bool
}

// DBReachable returns the stub DB health flag.
func (h *Health) DBReachable(_ context.Context) bool { return h.DB }

// FortnoxReachable returns the stub Fortnox health flag.
func (h *Health) FortnoxReachable(_ context.Context) bool { return h.Fortnox }

// AllHealthy returns a Health stub where everything is reachable.
func AllHealthy() *Health { return &Health{DB: true, Fortnox: true} }
