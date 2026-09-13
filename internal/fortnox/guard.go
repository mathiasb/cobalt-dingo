package fortnox

import (
	"errors"
	"fmt"
	"strings"
)

// ErrWrongCompany is returned by AssertCompany when the token opens a
// different company than the caller expected.
var ErrWrongCompany = errors.New("WRONG COMPANY: this token does not open the expected company")

// ErrNoExpectedCompany is returned when no expectation was configured. It is
// an error rather than a permissive default on purpose — see AssertCompany.
var ErrNoExpectedCompany = errors.New("no expected company configured: refusing to write to books nobody named")

// AssertCompany asks Fortnox which company an access token opens and returns
// an error unless it is the expected one.
//
// Why this exists. "Sandbox" is not a Fortnox environment — it is a value of
// FORTNOX_MODE in our own process. Both modes call api.fortnox.se
// (config.Fortnox.BaseURL), so the only thing separating test books from real
// books is which company was selected on the Fortnox consent screen.
//
// A Fortnox Developer licence creates test companies under the licence
// holder's own organisation number, so the number identifies neither. Observed
// live: "Definitely Mabe AB" (the production company) and "TEST Cobalt Dingo"
// (a test company) both report 556836-0688. One mis-click on a consent screen
// listing them adjacently puts a production token wherever a test token was
// meant to go, and every downstream check that reads FORTNOX_MODE agrees it is
// sandbox.
//
// So this asks the only authority that knows. It is required before any
// destructive tooling builds a writable client, and it fails closed on an
// unset expectation, an unreadable company, and any name mismatch.
//
// The client used here is read-only, so the check cannot itself be the thing
// that writes to the wrong company.
func AssertCompany(baseURL, accessToken, expected string) error {
	want := strings.TrimSpace(expected)
	if want == "" {
		return ErrNoExpectedCompany
	}

	info, err := NewClient(baseURL, accessToken, true).GetCompanyInfo()
	if err != nil {
		// Fail closed. An unreadable company is not permission to proceed.
		return fmt.Errorf("verify company before writing: %w", err)
	}

	got := strings.TrimSpace(info.CompanyName)
	if got != want {
		return fmt.Errorf("%w: token opens %q (org %s), expected %q",
			ErrWrongCompany, got, info.OrganizationNumber, want)
	}
	return nil
}
