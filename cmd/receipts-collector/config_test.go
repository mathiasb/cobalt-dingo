package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// The example config is the only instruction anyone has for writing the real
// one. If it does not load into this binary's own structs, following the
// documented steps produces a collector that reads no accounts, matches no
// rules, forwards nothing — and reports success while doing it.
//
// yaml.v3 ignores unknown fields by default, so a schema mismatch is silent.
// That is precisely why this needs asserting rather than eyeballing.
func TestExampleConfigLoadsIntoTheStructsThisBinaryUses(t *testing.T) {
	raw, err := os.ReadFile(exampleConfigPath)
	require.NoError(t, err, "the example config must exist where the docs say it does")

	var cfg configFile
	require.NoError(t, yaml.Unmarshal(raw, &cfg), "the example config must be valid YAML")

	require.NotEmpty(t, cfg.Sources, "example config yielded no mail sources")
	require.NotEmpty(t, cfg.Destinations, "example config yielded no destinations")
	require.NotEmpty(t, cfg.Rules, "example config yielded no routing rules")

	for _, s := range cfg.Sources {
		assert.NotEmpty(t, s.Name, "every source needs a name")
		assert.NotEmpty(t, s.Host, "source %q has no IMAP host", s.Name)
		assert.NotEmpty(t, s.PasswordEnv,
			"source %q must name an env var for its password, never inline it", s.Name)
	}

	for _, r := range cfg.Rules {
		assert.NotEmpty(t, r.Name, "every rule needs a name")
		assert.True(t, len(r.MatchFrom) > 0 || len(r.MatchSubject) > 0,
			"rule %q matches on nothing", r.Name)
	}

	// Every rule must point at a destination that exists, or it silently
	// becomes an unmatched mail at run time.
	known := map[string]bool{}
	for _, d := range cfg.Destinations {
		known[d.Name] = true
	}
	for _, r := range cfg.Rules {
		if r.Destination == "" {
			continue // explicit drop
		}
		assert.True(t, known[r.Destination],
			"rule %q routes to unknown destination %q", r.Name, r.Destination)
	}
}
