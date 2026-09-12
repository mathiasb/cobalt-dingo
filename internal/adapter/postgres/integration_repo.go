package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// IntegrationRepo implements domain.IntegrationStore using PostgreSQL.
//
// The client secret is stored sealed and is passed through untouched: this
// layer never decrypts, so a database dump and a log line are both useless
// without the master key from AgentSecrets.
type IntegrationRepo struct{ s *Store }

// NewIntegrationRepo returns an IntegrationRepo backed by s.
func NewIntegrationRepo(s *Store) *IntegrationRepo { return &IntegrationRepo{s: s} }

// Get implements domain.IntegrationStore.
//
// A missing row is domain.ErrNoIntegration — a normal state meaning this owner
// uses the application-level credentials. It is deliberately distinct from a
// query failure, which the caller must not read as "has none".
func (r *IntegrationRepo) Get(ctx context.Context, ownerID, mode string) (domain.Integration, error) {
	row, err := r.s.queries.GetIntegration(ctx, pgstore.GetIntegrationParams{
		OwnerID: ownerID, Mode: mode,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Integration{}, domain.ErrNoIntegration
	}
	if err != nil {
		return domain.Integration{}, fmt.Errorf("get integration: %w", err)
	}
	return domain.Integration{
		OwnerID:            row.OwnerID,
		Mode:               row.Mode,
		ClientID:           row.ClientID,
		ClientSecretSealed: row.ClientSecretSealed,
	}, nil
}

// Upsert stores an owner's integration. The secret must already be sealed —
// this layer takes no cipher, so it cannot accidentally write a plaintext one.
func (r *IntegrationRepo) Upsert(ctx context.Context, in domain.Integration) error {
	if in.ClientSecretSealed == "" || in.ClientID == "" {
		return errors.New("refusing to store an incomplete integration")
	}
	if err := r.s.queries.UpsertIntegration(ctx, pgstore.UpsertIntegrationParams{
		OwnerID: in.OwnerID, Mode: in.Mode,
		ClientID: in.ClientID, ClientSecretSealed: in.ClientSecretSealed,
	}); err != nil {
		return fmt.Errorf("upsert integration: %w", err)
	}
	return nil
}

// Delete removes an owner's integration, returning them to the
// application-level credentials.
func (r *IntegrationRepo) Delete(ctx context.Context, ownerID, mode string) error {
	if err := r.s.queries.DeleteIntegration(ctx, pgstore.DeleteIntegrationParams{
		OwnerID: ownerID, Mode: mode,
	}); err != nil {
		return fmt.Errorf("delete integration: %w", err)
	}
	return nil
}
