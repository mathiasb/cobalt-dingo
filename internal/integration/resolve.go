// Package integration resolves which Fortnox connected app a request uses.
//
// Per ADR-0005 each tenant may register their own private Fortnox integration.
// Resolution is therefore a chain: the owner's own credentials if they have
// registered any, otherwise the application-level ones from configuration. That
// keeps single-tenant deployments working with no database and no migration.
package integration

import (
	"context"
	"errors"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// Resolve returns the Fortnox credentials to use for ownerID.
//
// The redirect URI and scopes always come from appLevel: they describe *this*
// application — where Fortnox sends the user back, and what this app asks for —
// not the tenant's credentials. Only the client id and secret are per-tenant.
//
// It fails rather than falling back in every case except one: the owner has no
// registered integration, which is a normal state. In particular a record that
// exists but cannot be decrypted is an ERROR, never a fallback — falling back
// there would silently connect a tenant through the operator's own Fortnox
// integration, which is a cross-tenant leak dressed as resilience.
func Resolve(
	ctx context.Context,
	store domain.IntegrationStore,
	cipher *crypto.Cipher,
	ownerID string,
	appLevel config.Fortnox,
) (config.Fortnox, error) {
	if store == nil {
		// No database: a single-tenant deployment. The application-level
		// credentials are the answer, not an error.
		return appLevel, nil
	}

	rec, err := store.Get(ctx, ownerID, string(appLevel.Mode))
	if errors.Is(err, domain.ErrNoIntegration) {
		return appLevel, nil
	}
	if err != nil {
		// A broken store is not an empty store. Reading it as "this owner has
		// none" would quietly move them onto our credentials.
		return config.Fortnox{}, fmt.Errorf("look up integration for owner: %w", err)
	}

	if rec.ClientID == "" || rec.ClientSecretSealed == "" {
		return config.Fortnox{}, errors.New("registered integration is incomplete: refusing to fall back to the application-level credentials, which belong to a different Fortnox account")
	}
	if cipher == nil {
		return config.Fortnox{}, errors.New("registered integration is encrypted but no encryption key is configured")
	}

	secret, err := cipher.Open(rec.ClientSecretSealed)
	if err != nil {
		return config.Fortnox{}, fmt.Errorf("decrypt registered client secret: %w", err)
	}

	out := appLevel
	out.ClientID = rec.ClientID
	out.ClientSecret = secret
	return out, nil
}
