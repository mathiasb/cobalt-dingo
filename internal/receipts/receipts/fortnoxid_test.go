package receipts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/receipts"
)

// baseConfig is a minimal valid config; each test breaks one thing.
func baseConfig() receipts.Config {
	return receipts.Config{
		FortnoxID: "1522508",
		Sources: []receipts.SourceConfig{
			{Name: "a", Host: "imap.example.com", Username: "u", PasswordEnv: "PW"},
		},
		Destinations: []receipts.DestinationConfig{
			{Name: "fortnox-invoices", Type: "smtp", Address: "inbox.lev.1522508@arkivplats.se"},
		},
		Rules: []receipts.RuleConfig{
			{Name: "r", MatchFrom: []string{"*@example.com"}, Destination: "fortnox-invoices"},
		},
	}
}

// cobalt-dingo#80. Ten supplier invoices went to an `inbox.lev.<other>` address
// after a Fortnox database migration. A wrong archive address does not bounce
// back into the workflow — the forward succeeds and the document never appears,
// which is indistinguishable from success.
//
// A typo in a config file is invisible. A mismatch against a declared
// identifier is not, and it is caught at load rather than at send.
func TestValidate_refusesAFortnoxAddressWithTheWrongIdentifier(t *testing.T) {
	cfg := baseConfig()
	cfg.Destinations[0].Address = "inbox.lev.9999999@arkivplats.se"

	err := cfg.Validate()

	require.Error(t, err, "a mismatched Fortnox identifier must fail the config, not the invoice")
	assert.Contains(t, err.Error(), "fortnox-invoices")
	assert.NotContains(t, err.Error(), "9999999",
		"the error must not echo the wrong identifier — errors reach logs and transcripts")
}

func TestValidate_acceptsAMatchingIdentifier(t *testing.T) {
	cfg := baseConfig()
	require.NoError(t, cfg.Validate())
}

func TestValidate_acceptsBothArchiveKinds(t *testing.T) {
	cfg := baseConfig()
	cfg.Destinations = append(cfg.Destinations, receipts.DestinationConfig{
		Name: "fortnox-receipts", Type: "smtp", Address: "inbox.ver.1522508@arkivplats.se",
	})

	require.NoError(t, cfg.Validate())
}

// Without a declared identifier every address looks equally plausible, which
// is the whole defect. An arkivplats destination and no fortnox_id is refused.
func TestValidate_refusesAnArchiveAddressWithNoDeclaredIdentifier(t *testing.T) {
	cfg := baseConfig()
	cfg.FortnoxID = ""

	err := cfg.Validate()

	require.Error(t, err)
	// Assert on the message unique to THIS branch. "fortnox_id" alone appears in
	// the mismatch error too, so the looser assertion passed even with this
	// check deleted — found by mutating it out.
	assert.Contains(t, err.Error(), "no `fortnox_id` is configured")
}

// The guard must not fire on unrelated destinations, or a config with no
// Fortnox archive at all becomes unloadable.
func TestValidate_ignoresNonArchiveDestinations(t *testing.T) {
	cfg := baseConfig()
	cfg.FortnoxID = ""
	cfg.Destinations = []receipts.DestinationConfig{
		{Name: "mynt", Type: "smtp", Address: "kvitto+token@mynt.se"},
	}
	cfg.Rules[0].Destination = "mynt"

	require.NoError(t, cfg.Validate())
}

// A malformed archive address is not the same as a mismatched one, and saying
// so is the difference between a two-second fix and a hunt.
func TestValidate_refusesAMalformedArchiveAddress(t *testing.T) {
	cfg := baseConfig()
	cfg.Destinations[0].Address = "inbox.lev@arkivplats.se" // no identifier at all

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "inbox.ver.<fortnox_id>")
}
