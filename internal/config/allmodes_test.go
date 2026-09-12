package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

func TestLoadAllModes_includesAFullyConfiguredMode(t *testing.T) {
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "id")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_SECRET", "secret")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "https://books.d-ma.be/fortnox/callback")

	modes, incomplete := config.LoadAllModes()

	require.Contains(t, modes, config.ModeProduction)
	assert.Empty(t, incomplete)
}

// A mode with a client id but no secret used to be offered in the UI and then
// failed at token exchange with an opaque "token exchange failed". Half a
// credential is not a usable mode, so it is omitted — and reported, because a
// silently missing mode is its own confusion.
func TestLoadAllModes_omitsAModeMissingItsSecret(t *testing.T) {
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "id")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_SECRET", "")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "https://books.d-ma.be/fortnox/callback")

	modes, incomplete := config.LoadAllModes()

	assert.NotContains(t, modes, config.ModeProduction,
		"an unusable mode must not be offered as connectable")
	require.Len(t, incomplete, 1)
	assert.Contains(t, incomplete[0], "CLIENT_SECRET")
	assert.NotContains(t, incomplete[0], "id", "the reason must not echo credential values")
}

func TestLoadAllModes_omitsAModeMissingItsRedirectURI(t *testing.T) {
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "id")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_SECRET", "secret")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "")

	modes, incomplete := config.LoadAllModes()

	assert.NotContains(t, modes, config.ModeProduction)
	require.Len(t, incomplete, 1)
	assert.Contains(t, incomplete[0], "REDIRECT_URI")
}

// A mode with no client id at all is simply not configured. That is the normal
// state for production before the credentials exist, and it must not be
// reported as a misconfiguration.
func TestLoadAllModes_saysNothingAboutAModeThatIsNotConfiguredAtAll(t *testing.T) {
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "")

	modes, incomplete := config.LoadAllModes()

	assert.NotContains(t, modes, config.ModeProduction)
	assert.Empty(t, incomplete, "absent is not the same as broken")
}
