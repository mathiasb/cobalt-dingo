package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

func TestLoadAllModes_readsTheServiceAccountType(t *testing.T) {
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "id")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_SECRET", "secret")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "https://books.d-ma.be/fortnox/callback")
	t.Setenv("FORTNOX_PRODUCTION_ACCOUNT_TYPE", "service")

	modes, incomplete := config.LoadAllModes()

	require.Empty(t, incomplete)
	assert.Equal(t, "service", modes[config.ModeProduction].AccountType)
}

// Unset means a normal user authorization, which is what the sandbox already
// has. Defaulting to service would silently invalidate an existing connection
// on its next re-authorization.
func TestLoadAllModes_accountTypeIsEmptyByDefault(t *testing.T) {
	t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "id")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_SECRET", "secret")
	t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "https://books.d-ma.be/fortnox/callback")

	modes, _ := config.LoadAllModes()

	assert.Empty(t, modes[config.ModeSandbox].AccountType)
}

// `service` is the ONLY valid value Fortnox documents. A typo would otherwise
// travel to the authorize endpoint and be rejected there, or worse ignored —
// producing a user-bound token while the operator believed they had a service
// account. That difference is invisible afterwards and costs a
// re-authorization to correct.
func TestLoadAllModes_refusesAnUnknownAccountType(t *testing.T) {
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "id")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_SECRET", "secret")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "https://books.d-ma.be/fortnox/callback")
	t.Setenv("FORTNOX_PRODUCTION_ACCOUNT_TYPE", "Service")

	modes, incomplete := config.LoadAllModes()

	assert.NotContains(t, modes, config.ModeProduction,
		"a mode with an invalid account type must not be offered")
	require.Len(t, incomplete, 1)
	assert.Contains(t, incomplete[0], "ACCOUNT_TYPE")
}
