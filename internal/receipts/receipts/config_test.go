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

// The routing rules were written by listing merchants. These cases come from
// ground truth instead: 209 receipts Mathias forwarded to the Mynt inbox by
// hand, audited 2026-09-09. Each address below is one he decided was worth
// booking and the rules missed.
//
// Four were outright pattern bugs that could never have matched anything.
func TestRulesMatchTheSendersActuallyForwarded(t *testing.T) {
	cfg, err := receipts.LoadConfig(repoPath(receipts.ExampleConfigPath))
	require.NoError(t, err)
	router := receipts.NewRouter(cfg.RuleList(), cfg.DestinationList())

	cases := []struct {
		what    string
		from    string
		subject string
	}{
		// The four bugs.
		{
			"Anthropic invoices come from a SUBDOMAIN, so *@anthropic.com never matched",
			"invoice+statements@mail.anthropic.com", "Your invoice from Anthropic",
		},
		{
			"Parkster is parkster.SE, not .com — the rule matched nothing",
			"no-reply-charging@parkster.se", "Kvitto",
		},
		{
			"Google payment receipts use subjects outside the listed set",
			"payments-noreply@google.com", "Your Google payment receipt",
		},
		{
			"Apple sends receipt subjects beyond the listed ones",
			"no_reply@email.apple.com", "Your receipt from Apple",
		},

		// Payment intermediaries — the merchant is in the subject, not the sender.
		{"Stripe", "invoice+statements+acct_1m07hslmdodimxbs@stripe.com", "Invoice paid"},
		{"Paddle", "help@paddle.com", "Your receipt"},
		{"Zettle", "no-reply@zettle.com", "Kvitto"},

		// Travel, parking, tolls — the real long tail.
		{"Uber", "noreply@uber.com", "Your Wednesday morning trip with Uber"},
		{"SAS", "no-reply@flysas.com", "Din bokning"},
		{"AimoPark", "no-reply@aimopark.io", "Kvitto parkering"},
		{"Booking.com", "noreply-payments@booking.com", "Your payment"},
		{"Omio", "service@omio.com", "Your booking confirmation"},
		{"ASFINAG road toll", "shop@asfinag.at", "Ihre Rechnung"},
		{"Taxi Stockholm", "noreply@taxistockholm.se", "Kvitto"},

		// SaaS.
		{"1Password", "hello@1password.com", "Your receipt"},
		{"Mistral", "no-reply@mistral.ai", "Invoice"},
		{"Mistral, second subdomain", "no-reply@emails.mistral.ai", "Invoice"},
		{"GitHub", "noreply@github.com", "Payment receipt"},
		{"Audible", "donotreply@audible.com", "Your receipt"},
	}

	for _, tc := range cases {
		t.Run(tc.from, func(t *testing.T) {
			dest, ok := router.Route(receipts.Mail{From: tc.from, Subject: tc.subject})
			require.True(t, ok, "no rule matched — %s", tc.what)
			assert.NotNil(t, dest)
		})
	}
}

// Widening the rules must not turn them into "route everything". A receipt
// rule that matches newsletters puts junk in the books, and the survey already
// showed a flat domain list doing exactly that: `bonniernews.se` alone matched
// 57 Expressen adverts.
func TestRulesStillRejectNonReceipts(t *testing.T) {
	cfg, err := receipts.LoadConfig(repoPath(receipts.ExampleConfigPath))
	require.NoError(t, err)
	router := receipts.NewRouter(cfg.RuleList(), cfg.DestinationList())

	nonReceipts := []struct{ from, subject string }{
		{"noreply-expressen@email.bonniernews.se", "Läs hela sommaren – 99 kr/mån i 3 månader!"},
		{"svd@utskick.svd.se", "Veckans nyhetsbrev"},
		{"noreply@notifications.kivra.com", "Kom ihåg att läsa brevet från Skatteverket"},
		{"newsletters@technologyreview.com", "The Download: today's news"},
		{"noreply@svenskalag.se", "Kallelse till match"},
	}

	for _, tc := range nonReceipts {
		t.Run(tc.from, func(t *testing.T) {
			if dest, ok := router.Route(receipts.Mail{From: tc.from, Subject: tc.subject}); ok {
				t.Errorf("routed a non-receipt to %q — this puts junk in the books", dest.Name)
			}
		})
	}
}

// Widening the rules by SENDER in #76 fixed a 46% miss rate and introduced a
// false-positive rate: Anthropic, 1Password and Berget all send security
// alerts and product mail from the same addresses as their invoices.
//
// Every case below was routed to bookkeeping in a live dry run on 2026-09-09.
// None is a receipt.
//
// The earlier negative test only covered newsletters, because the positive
// cases were drawn from the forwarded set — ground truth told us what to
// catch and nothing told us what to avoid.
func TestSecurityAlertsAndNoticesAreNotRouted(t *testing.T) {
	cfg, err := receipts.LoadConfig(repoPath(receipts.ExampleConfigPath))
	require.NoError(t, err)
	router := receipts.NewRouter(cfg.RuleList(), cfg.DestinationList())

	notReceipts := []struct{ what, from, subject string }{
		{"1Password sign-in alert", "hello@1password.com", "New 1Password sign-in alert"},
		{
			"Claude device alert", "no-reply-i-8WdaVL4WaAnqoEP5iHig@mail.anthropic.com",
			"Security alert: new trusted device added to your Claude account",
		},
		{
			"Claude passkey alert", "no-reply-jhTwecdb9AxMhkbIec4erw@mail.anthropic.com",
			"Security alert: new passkey added to your Claude account",
		},
		{
			"Berget product announcement", "andreas@berget.ai",
			"Early access: Kimi K3 is available for evaluation on Berget",
		},
		{
			"a cancellation is not a purchase", "noreply@booking.com",
			"Booking cancelled for Hotel Sonnenheim",
		},
		{
			"a notice that no invoice was raised", "billing@hetzner.com",
			"Minimum invoice amount not reached (K0499337726)",
		},

		// A failed payment means the charge did NOT succeed, so there is no
		// expense to book — routing it would record a purchase that never
		// happened. An earlier test here asserted the opposite; that was my
		// error. This belongs to forfall, which watches unmet obligations.
		{
			"a failed payment is not an expense",
			"failed-payments@mail.anthropic.com", "Payment failed",
		},

		// A pre-notification of a charge that has not happened yet. Same
		// category as a failed payment: booking it records an expense that
		// does not exist. The real receipt follows once the card is charged.
		{
			"an upcoming charge is not yet an expense",
			"no-reply-charging@parkster.se", "Kommande kortbetalning",
		},
	}

	for _, tc := range notReceipts {
		t.Run(tc.what, func(t *testing.T) {
			if dest, ok := router.Route(receipts.Mail{From: tc.from, Subject: tc.subject}); ok {
				t.Errorf("routed to %q — %s is not a receipt and this puts junk in the books",
					dest.Name, tc.what)
			}
		})
	}
}

// Tightening must not undo #76. The real receipts from those same senders have
// to keep routing, or the fix trades one failure for the other.
func TestRealReceiptsFromTheSameSendersStillRoute(t *testing.T) {
	cfg, err := receipts.LoadConfig(repoPath(receipts.ExampleConfigPath))
	require.NoError(t, err)
	router := receipts.NewRouter(cfg.RuleList(), cfg.DestinationList())

	receiptsFromMixedSenders := []struct{ from, subject string }{
		{"invoice+statements@mail.anthropic.com", "Your receipt from Anthropic, PBC #2879-8484-5252"},
		{"hello@1password.com", "Your receipt from 1Password"},
		{"info@berget.ai", "Faktura 12345"},
		{"noreply@booking.com", "Thanks! Your booking is confirmed at Hotel Sonnenheim"},
		{"billing@hetzner.com", "Your Hetzner invoice 12345"},
	}

	for _, tc := range receiptsFromMixedSenders {
		t.Run(tc.subject, func(t *testing.T) {
			if _, ok := router.Route(receipts.Mail{From: tc.from, Subject: tc.subject}); !ok {
				t.Errorf("a real receipt stopped routing — the tightening went too far")
			}
		})
	}
}
