package receipts_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/receipts"
)

// repoPath resolves a repo-relative path from this package's directory.
func repoPath(rel string) string {
	return filepath.Join("..", "..", "..", rel)
}

// The example config is the only instruction anyone has for writing the real
// one. If it does not load into the types the binaries use, following the
// documented steps produces a collector that reads no accounts, matches no
// rules, forwards nothing — and reports success while doing it.
//
// yaml.v3 ignores unknown fields, so schema drift is silent. That is exactly
// what happened before: the example described `accounts:` with nested `imap:`
// blocks while the loader wanted `sources:` with flat fields, and nothing
// noticed because nothing asserted it.
func TestExampleConfigLoads(t *testing.T) {
	cfg, err := receipts.LoadConfig(repoPath(receipts.ExampleConfigPath))
	require.NoError(t, err, "the committed example must load and validate")

	assert.NotEmpty(t, cfg.Sources)
	assert.NotEmpty(t, cfg.Destinations)
	assert.NotEmpty(t, cfg.Rules)

	for _, s := range cfg.Sources {
		assert.NotEmpty(t, s.PasswordEnv,
			"source %q must name an env var, never inline a password", s.Name)
	}
}

// Validate must reject configurations that parse but do nothing, because that
// is the failure this whole file exists to prevent.
func TestValidateRejectsUselessConfigs(t *testing.T) {
	full := func() *receipts.Config {
		return &receipts.Config{
			Sources: []receipts.SourceConfig{{
				Name: "a", Host: "imap.example.com", Username: "u", PasswordEnv: "PW",
			}},
			Destinations: []receipts.DestinationConfig{{Name: "d", Type: "smtp"}},
			Rules: []receipts.RuleConfig{{
				Name: "r", MatchFrom: []string{"*@example.com"}, Destination: "d",
			}},
		}
	}

	require.NoError(t, full().Validate(), "the baseline must be valid")

	tests := []struct {
		name   string
		mutate func(*receipts.Config)
		want   string
	}{
		{"no sources", func(c *receipts.Config) { c.Sources = nil }, "no mail sources"},
		{"no destinations", func(c *receipts.Config) { c.Destinations = nil }, "no destinations"},
		{"no rules", func(c *receipts.Config) { c.Rules = nil }, "no routing rules"},
		{
			"inline password instead of env var",
			func(c *receipts.Config) { c.Sources[0].PasswordEnv = "" },
			"password_env",
		},
		{
			"rule matches nothing",
			func(c *receipts.Config) { c.Rules[0].MatchFrom = nil; c.Rules[0].MatchSubject = nil },
			"matches on nothing",
		},
		{
			"rule points at a destination that does not exist",
			func(c *receipts.Config) { c.Rules[0].Destination = "typo" },
			"unknown destination",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := full()
			tc.mutate(c)
			err := c.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// An explicit drop rule has an empty destination and must stay valid — it is
// how "ignore this sender" is expressed.
func TestExplicitDropRuleIsValid(t *testing.T) {
	c := &receipts.Config{
		Sources: []receipts.SourceConfig{{
			Name: "a", Host: "h", Username: "u", PasswordEnv: "PW",
		}},
		Destinations: []receipts.DestinationConfig{{Name: "d", Type: "smtp"}},
		Rules: []receipts.RuleConfig{{
			Name: "drop", MatchFrom: []string{"*@noise.example"}, Destination: "",
		}},
	}
	assert.NoError(t, c.Validate())
}
