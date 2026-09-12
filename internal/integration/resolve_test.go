package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/integration"
)

const testKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

type fakeStore struct {
	rec domain.Integration
	err error
}

func (f fakeStore) Get(context.Context, string, string) (domain.Integration, error) {
	return f.rec, f.err
}

func appLevel() config.Fortnox {
	return config.Fortnox{
		Mode: config.ModeProduction, ClientID: "app-level-id", ClientSecret: "app-level-secret",
		RedirectURI: "https://books.d-ma.be/fortnox/callback", Scopes: "bookkeeping",
	}
}

func sealed(t *testing.T, plaintext string) string {
	t.Helper()
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)
	s, err := c.Seal(plaintext)
	require.NoError(t, err)
	return s
}

// A tenant who has registered their own integration uses it, not ours.
func TestResolve_prefersTheTenantsOwnIntegration(t *testing.T) {
	store := fakeStore{rec: domain.Integration{
		ClientID: "tenant-id", ClientSecretSealed: sealed(t, "tenant-secret"),
	}}
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	got, err := integration.Resolve(context.Background(), store, c, "owner-1", appLevel())

	require.NoError(t, err)
	assert.Equal(t, "tenant-id", got.ClientID)
	assert.Equal(t, "tenant-secret", got.ClientSecret)
	assert.Equal(t, appLevel().RedirectURI, got.RedirectURI,
		"the redirect URI and scopes are ours, not the tenant's — they describe our app, not their credentials")
	assert.Equal(t, appLevel().Scopes, got.Scopes)
}

// The operator's own use, and any tenant who has not registered one.
func TestResolve_fallsBackToTheAppLevelIntegration(t *testing.T) {
	store := fakeStore{err: domain.ErrNoIntegration}
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	got, err := integration.Resolve(context.Background(), store, c, "owner-1", appLevel())

	require.NoError(t, err)
	assert.Equal(t, "app-level-id", got.ClientID)
	assert.Equal(t, "app-level-secret", got.ClientSecret)
}

// The important one. A record that exists but cannot be decrypted must NOT
// fall back to the app-level credentials: that would silently connect a
// tenant through the operator's own Fortnox integration, which is a
// cross-tenant leak dressed up as resilience.
func TestResolve_refusesToFallBackWhenTheTenantsRecordCannotBeDecrypted(t *testing.T) {
	store := fakeStore{rec: domain.Integration{
		ClientID: "tenant-id", ClientSecretSealed: "not-decryptable",
	}}
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	_, err = integration.Resolve(context.Background(), store, c, "owner-1", appLevel())

	require.Error(t, err, "an undecryptable tenant record must fail the request, not borrow our credentials")
	assert.NotContains(t, err.Error(), "app-level-secret")
}

// A store that is broken is not a store that is empty.
func TestResolve_refusesWhenTheStoreFails(t *testing.T) {
	store := fakeStore{err: errors.New("connection refused")}
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	_, err = integration.Resolve(context.Background(), store, c, "owner-1", appLevel())

	require.Error(t, err, "a database error must not be read as `this tenant has no integration`")
}

// With no store configured at all — a single-tenant deployment with no
// database — the app-level integration is the answer, not an error.
func TestResolve_worksWithNoStoreAtAll(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	got, err := integration.Resolve(context.Background(), nil, c, "owner-1", appLevel())

	require.NoError(t, err)
	assert.Equal(t, "app-level-id", got.ClientID)
}

// A tenant record with an empty sealed secret is a broken row, not a licence
// to use ours.
func TestResolve_refusesARecordWithNoSecret(t *testing.T) {
	store := fakeStore{rec: domain.Integration{ClientID: "tenant-id", ClientSecretSealed: ""}}
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	_, err = integration.Resolve(context.Background(), store, c, "owner-1", appLevel())

	require.Error(t, err)
}

// Without a cipher, an encrypted record cannot be used — and must not be
// bypassed.
func TestResolve_refusesAnEncryptedRecordWithNoCipher(t *testing.T) {
	store := fakeStore{rec: domain.Integration{ClientID: "tenant-id", ClientSecretSealed: "whatever"}}

	_, err := integration.Resolve(context.Background(), store, nil, "owner-1", appLevel())

	require.Error(t, err)
}
