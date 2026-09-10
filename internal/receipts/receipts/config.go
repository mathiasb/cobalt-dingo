package receipts

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath is where the collector and the connectivity check both look
// for the real (gitignored) configuration.
const DefaultConfigPath = "config/receipt-sources.yml"

// ExampleConfigPath is the committed template users copy. It is asserted to load
// into the types below, because yaml.v3 ignores unknown fields: an example that
// has drifted from these structs parses to an empty config and yields a
// collector that runs, forwards nothing, and reports success.
const ExampleConfigPath = "config/receipts/receipt-sources.example.yml"

// Config is the on-disk routing configuration.
//
// It lives here rather than in either cmd/ package so the collector and the
// connectivity check cannot disagree about the schema. They already did once —
// the example file described a shape no binary could read.
type Config struct {
	// FortnoxID is the Fortnox database identifier that archive addresses must
	// carry. Declared once so a typo'd or stale `inbox.ver.<id>@arkivplats.se`
	// fails the config rather than the invoice (#80): mail to the wrong archive
	// address does not bounce back into the workflow — the forward succeeds and
	// the document never appears.
	FortnoxID string `yaml:"fortnox_id"`

	Sources      []SourceConfig      `yaml:"sources"`
	Destinations []DestinationConfig `yaml:"destinations"`
	Rules        []RuleConfig        `yaml:"rules"`
}

// SourceConfig is one IMAP account. The password is never stored here — only
// the NAME of the environment variable holding it.
type SourceConfig struct {
	Name        string `yaml:"name"`
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	Username    string `yaml:"username"`
	PasswordEnv string `yaml:"password_env"`
	Folder      string `yaml:"folder"`
	TLS         bool   `yaml:"tls"`
}

type DestinationConfig struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Address string `yaml:"address"`
}

type RuleConfig struct {
	Name         string   `yaml:"name"`
	MatchFrom    []string `yaml:"match_from"`
	MatchSubject []string `yaml:"match_subject"`
	Destination  string   `yaml:"destination"`
}

// LoadConfig reads and validates a configuration file.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied config path
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// Validate rejects a configuration that would load without error but do nothing
// useful. An empty section is the signature of a file whose schema has drifted,
// and it must be an error rather than a quiet no-op.
func (c *Config) Validate() error {
	if len(c.Sources) == 0 {
		return fmt.Errorf("no mail sources configured")
	}
	if len(c.Destinations) == 0 {
		return fmt.Errorf("no destinations configured")
	}
	if len(c.Rules) == 0 {
		return fmt.Errorf("no routing rules configured")
	}

	for _, s := range c.Sources {
		switch {
		case s.Name == "":
			return fmt.Errorf("a source has no name")
		case s.Host == "":
			return fmt.Errorf("source %q has no host", s.Name)
		case s.Username == "":
			return fmt.Errorf("source %q has no username", s.Name)
		case s.PasswordEnv == "":
			return fmt.Errorf("source %q must set password_env, never an inline password", s.Name)
		}
	}

	known := make(map[string]bool, len(c.Destinations))
	for _, d := range c.Destinations {
		if d.Name == "" {
			return fmt.Errorf("a destination has no name")
		}
		if err := c.checkArchiveAddress(d); err != nil {
			return err
		}
		known[d.Name] = true
	}

	for _, r := range c.Rules {
		if r.Name == "" {
			return fmt.Errorf("a rule has no name")
		}
		if len(r.MatchFrom) == 0 && len(r.MatchSubject) == 0 {
			return fmt.Errorf("rule %q matches on nothing", r.Name)
		}
		if r.Destination == "" {
			continue // explicit drop
		}
		if !known[r.Destination] {
			return fmt.Errorf("rule %q routes to unknown destination %q", r.Name, r.Destination)
		}
	}
	return nil
}

// Sources resolves each configured account's password from the environment.
// The password is read straight into the returned struct and is never logged.
func (c *Config) SourceList() ([]*Source, error) {
	out := make([]*Source, 0, len(c.Sources))
	for _, s := range c.Sources {
		password := os.Getenv(s.PasswordEnv)
		if password == "" {
			return nil, fmt.Errorf("source %q: environment variable %s is not set", s.Name, s.PasswordEnv)
		}
		out = append(out, &Source{
			Name:     s.Name,
			Host:     s.Host,
			Port:     s.Port,
			Username: s.Username,
			Password: password,
			Folder:   s.Folder,
			TLS:      s.TLS,
		})
	}
	return out, nil
}

// DestinationList converts the configured destinations.
func (c *Config) DestinationList() []*Destination {
	out := make([]*Destination, 0, len(c.Destinations))
	for _, d := range c.Destinations {
		out = append(out, &Destination{Name: d.Name, Type: d.Type, Address: d.Address})
	}
	return out
}

// RuleList converts the configured rules.
func (c *Config) RuleList() []Rule {
	out := make([]Rule, 0, len(c.Rules))
	for _, r := range c.Rules {
		// RuleConfig and Rule are structurally identical today. They are kept
		// separate deliberately: one is the on-disk contract, the other the
		// domain type, and letting the wire format dictate the domain shape is
		// how a config change silently becomes a behaviour change.
		out = append(out, Rule(r))
	}
	return out
}

// archiveAddress matches Fortnox's document-archive addresses.
//
// `inbox.ver.<id>` is verifikat (receipts) and `inbox.lev.<id>` is supplier
// invoices. The identifier is the Fortnox database, and it changes when the
// database does — which is what happened in spring 2024, leaving a config
// pointing at the previous one (#80).
var archiveAddress = regexp.MustCompile(`^inbox\.(ver|lev)\.([0-9]+)@arkivplats\.se$`)

// arkivplatsSuffix identifies an archive destination even when its address is
// malformed, so a broken one is reported rather than silently ignored.
const arkivplatsSuffix = "@arkivplats.se"

// checkArchiveAddress refuses a Fortnox archive destination that does not carry
// the declared identifier.
//
// Deliberately at config-validation time, not at send time. A wrong archive
// address produces no error and no result: the forward succeeds, the document
// never appears in Fortnox, and nothing distinguishes that from success. The
// only place it can be caught is before anything sends.
//
// The identifier is never echoed into the error. Errors reach logs and
// transcripts, and a Fortnox database id does not belong in either.
func (c *Config) checkArchiveAddress(d DestinationConfig) error {
	addr := strings.ToLower(strings.TrimSpace(d.Address))
	if !strings.HasSuffix(addr, arkivplatsSuffix) {
		return nil
	}
	if strings.TrimSpace(c.FortnoxID) == "" {
		return fmt.Errorf("destination %q is a Fortnox archive address but no `fortnox_id` is configured: without the expected identifier every address looks equally plausible, which is how ten supplier invoices went to a retired database", d.Name)
	}
	m := archiveAddress.FindStringSubmatch(addr)
	if m == nil {
		return fmt.Errorf("destination %q is not a valid Fortnox archive address: expected inbox.ver.<fortnox_id>@arkivplats.se or inbox.lev.<fortnox_id>@arkivplats.se", d.Name)
	}
	if m[2] != strings.TrimSpace(c.FortnoxID) {
		return fmt.Errorf("destination %q carries a Fortnox identifier that is not the configured `fortnox_id`: mail to the wrong archive does not bounce, it simply never arrives — check whether the Fortnox database changed", d.Name)
	}
	return nil
}
