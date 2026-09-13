package fortnox

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// companyServer serves /3/companyinformation for the named company and records
// whether anything tried to write.
func companyServer(t *testing.T, name, orgNumber string) (*httptest.Server, *bool) {
	t.Helper()
	wrote := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			wrote = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"CompanyInformation":{"CompanyName":"` + name + `","OrganizationNumber":"` + orgNumber + `"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &wrote
}

// The hazard this closes: "sandbox" is a label in our own environment, not a
// Fortnox environment — both modes call api.fortnox.se, and the only thing
// separating test books from real books is which company was picked on the
// consent screen. The organisation number cannot tell them apart when a
// Fortnox Developer licence has created test companies under the licence
// holder's own number.
func TestAssertCompany_refusesWhenTheTokenBelongsToADifferentCompany(t *testing.T) {
	srv, wrote := companyServer(t, "Definitely Mabe AB", "556836-0688")

	err := AssertCompany(srv.URL, "tok", "TEST Cobalt Dingo")

	require.Error(t, err, "a token for the production company must not pass a test-company check")
	assert.True(t, errors.Is(err, ErrWrongCompany))
	assert.Contains(t, err.Error(), "Definitely Mabe AB", "the error must name the company the token actually opens")
	assert.Contains(t, err.Error(), "TEST Cobalt Dingo", "and the one that was expected")
	assert.False(t, *wrote, "the check itself must never write")
}

func TestAssertCompany_acceptsTheExpectedCompany(t *testing.T) {
	srv, _ := companyServer(t, "TEST Cobalt Dingo", "556836-0688")
	assert.NoError(t, AssertCompany(srv.URL, "tok", "TEST Cobalt Dingo"))
}

// Fail closed on an unconfigured expectation. Treating "" as "anything is
// fine" would mean a forgotten environment variable silently restores the
// original hazard.
func TestAssertCompany_refusesAnEmptyExpectation(t *testing.T) {
	srv, _ := companyServer(t, "TEST Cobalt Dingo", "556836-0688")
	err := AssertCompany(srv.URL, "tok", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoExpectedCompany))
}

// Names differing only by surrounding whitespace are the same company; names
// differing in any other way are not, and case is not a safe thing to fold
// when two real companies are called "Definitely Mabe AB" and the test one is
// "TEST Cobalt Dingo".
func TestAssertCompany_comparesExactlyAfterTrimmingWhitespace(t *testing.T) {
	srv, _ := companyServer(t, "  TEST Cobalt Dingo  ", "556836-0688")
	assert.NoError(t, AssertCompany(srv.URL, "tok", "TEST Cobalt Dingo"))

	srv2, _ := companyServer(t, "TEST Cobalt Dingo 2", "556836-0688")
	assert.Error(t, AssertCompany(srv2.URL, "tok", "TEST Cobalt Dingo"),
		"a longer name that merely starts the same is a different company")
}

// An unreachable or erroring Fortnox must block the write, not wave it through.
func TestAssertCompany_refusesWhenTheCompanyCannotBeRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	assert.Error(t, AssertCompany(srv.URL, "tok", "TEST Cobalt Dingo"))
}
