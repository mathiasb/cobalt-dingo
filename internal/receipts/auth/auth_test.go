package auth_test

import (
	"os"
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Token behaviour ---

func TestToken_IsExpired_WhenExpiryInPast(t *testing.T) {
	tok := &auth.Token{Expiry: time.Now().Add(-time.Hour)}
	assert.True(t, tok.IsExpired())
}

func TestToken_IsExpired_WhenExpiryFarFuture(t *testing.T) {
	tok := &auth.Token{Expiry: time.Now().Add(time.Hour)}
	assert.False(t, tok.IsExpired())
}

func TestToken_IsExpired_AppliesSixtySecondBuffer(t *testing.T) {
	// Token expires in 30s – within the 60s buffer, so should be "expired".
	tok := &auth.Token{Expiry: time.Now().Add(30 * time.Second)}
	assert.True(t, tok.IsExpired(), "token within 60s of expiry must be treated as expired")
}

// --- FileTokenStore contract ---

func TestFileTokenStore_LoadReturnsErrNoTokenWhenEmpty(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Load()
	assert.ErrorIs(t, err, auth.ErrNoToken)
}

func TestFileTokenStore_SaveAndLoadRoundTrip(t *testing.T) {
	store := newTestStore(t)
	expiry := time.Now().Add(time.Hour).Truncate(time.Second)

	original := &auth.Token{
		AccessToken:  "acc-abc123",
		RefreshToken: "ref-xyz789",
		TokenType:    "Bearer",
		Expiry:       expiry,
	}
	require.NoError(t, store.Save(original))

	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, original.AccessToken, loaded.AccessToken)
	assert.Equal(t, original.RefreshToken, loaded.RefreshToken)
	assert.Equal(t, original.TokenType, loaded.TokenType)
	assert.True(t, original.Expiry.Equal(loaded.Expiry), "expiry must survive round-trip")
}

func TestFileTokenStore_SaveOverwritesPreviousToken(t *testing.T) {
	store := newTestStore(t)

	require.NoError(t, store.Save(&auth.Token{AccessToken: "first"}))
	require.NoError(t, store.Save(&auth.Token{AccessToken: "second"}))

	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "second", loaded.AccessToken, "Save must overwrite, not append")
}

func TestFileTokenStore_TokenIsEncryptedOnDisk(t *testing.T) {
	path := t.TempDir() + "/tokens.enc"
	key := make([]byte, 32)
	store, err := auth.NewFileTokenStore(path, key)
	require.NoError(t, err)

	secret := &auth.Token{AccessToken: "super-secret-token"}
	require.NoError(t, store.Save(secret))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "super-secret-token",
		"plaintext access token must not appear in the stored file")
}

func TestFileTokenStore_DifferentKeyCannotDecrypt(t *testing.T) {
	path := t.TempDir() + "/tokens.enc"

	keyA := make([]byte, 32)
	keyA[0] = 0xAA
	storeA, err := auth.NewFileTokenStore(path, keyA)
	require.NoError(t, err)
	require.NoError(t, storeA.Save(&auth.Token{AccessToken: "secret"}))

	keyB := make([]byte, 32)
	keyB[0] = 0xBB
	storeB, err := auth.NewFileTokenStore(path, keyB)
	require.NoError(t, err)

	_, err = storeB.Load()
	assert.Error(t, err, "token encrypted with key A must not be decryptable with key B")
}

func TestFileTokenStore_ImplementsTokenStoreInterface(_ *testing.T) {
	var _ auth.TokenStore = (*auth.FileTokenStore)(nil)
}

// --- MemoryTokenStore (test helper) ---

func TestMemoryTokenStore_ImplementsTokenStoreInterface(_ *testing.T) {
	var _ auth.TokenStore = (*auth.MemoryTokenStore)(nil)
}

func TestMemoryTokenStore_LoadReturnsErrNoTokenWhenEmpty(t *testing.T) {
	store := &auth.MemoryTokenStore{}
	_, err := store.Load()
	assert.ErrorIs(t, err, auth.ErrNoToken)
}

func TestMemoryTokenStore_SaveAndLoad(t *testing.T) {
	store := &auth.MemoryTokenStore{}
	tok := &auth.Token{AccessToken: "mem-token"}

	require.NoError(t, store.Save(tok))
	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "mem-token", loaded.AccessToken)
}

// --- helpers ---

func newTestStore(t *testing.T) *auth.FileTokenStore {
	t.Helper()
	path := t.TempDir() + "/tokens.enc"
	key := make([]byte, 32) // all-zero key is fine for tests
	store, err := auth.NewFileTokenStore(path, key)
	require.NoError(t, err)
	return store
}
