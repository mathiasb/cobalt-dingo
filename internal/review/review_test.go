package review_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/review"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRubric = `# Review rubric

## R-SEC-01 — critical — Secret material appears in the diff
Body prose the parser ignores.

## R-MONEY-02 — high — Money handled as a float
More prose.

## R-STYLE-07 — low — Exported symbol lacks a doc comment
`

func TestParseRubric_ReadsIDsAndSeverities(t *testing.T) {
	r, err := review.ParseRubric([]byte(testRubric))
	require.NoError(t, err)

	require.Len(t, r.Rules, 3)
	assert.Equal(t, review.Critical, r.Rules["R-SEC-01"].Severity)
	assert.Equal(t, review.High, r.Rules["R-MONEY-02"].Severity)
	assert.Equal(t, review.Low, r.Rules["R-STYLE-07"].Severity)
	assert.Equal(t, "Secret material appears in the diff", r.Rules["R-SEC-01"].Title)
}

func TestParseRubric_RejectsMalformedRule(t *testing.T) {
	tests := map[string]string{
		"unknown severity":  "## R-SEC-01 — screaming — Something\n",
		"missing severity":  "## R-SEC-01 — Something\n",
		"malformed id":      "## SEC-1 — high — Something\n",
		"duplicate rule id": testRubric + "\n## R-SEC-01 — low — Redefined\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := review.ParseRubric([]byte(body))
			require.Error(t, err)
		})
	}
}

func TestParseRubric_RejectsEmptyRubric(t *testing.T) {
	// A rubric that parses to zero rules would make every review vacuously
	// clean. That is the "no output passes as green" failure in another costume.
	_, err := review.ParseRubric([]byte("# Review rubric\n\nNo rules here.\n"))
	require.Error(t, err)
}

// The shipped rubric is test input for the real gate, so a broken one must fail
// `task check` rather than fail silently in CI.
func TestParseRubric_TheShippedRubricParses(t *testing.T) {
	path := filepath.Join("..", "..", ".review", "rubric.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "the rubric the workflow loads must exist")

	r, err := review.ParseRubric(raw)
	require.NoError(t, err)
	assert.NotEmpty(t, r.Rules)
}

func TestParseFindings_FailsClosedOnNoOutput(t *testing.T) {
	// accounted's own failure: `workflow_run` jobs produced no review for 10
	// consecutive PRs and nobody noticed, because "no output" exited 0. An empty
	// model response is a broken reviewer, not a clean diff.
	rubric := mustRubric(t)

	for name, raw := range map[string]string{
		"empty":      "",
		"whitespace": "   \n\t\n",
		"not json":   "Looks good to me!",
		"null":       "null",
		"wrong type": `{"findings": "none"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := review.ParseFindings([]byte(raw), rubric)
			require.Error(t, err)
		})
	}
}

func TestParseFindings_EmptyFindingsListIsACleanReview(t *testing.T) {
	// Distinct from the case above and the distinction is the whole point:
	// "I reviewed it and found nothing" is a verdict; "I produced nothing" is not.
	rep, err := review.ParseFindings([]byte(`{"findings": []}`), mustRubric(t))
	require.NoError(t, err)
	assert.Empty(t, rep.Findings)
	assert.True(t, rep.Reviewed)
}

func TestParseFindings_ToleratesFencedJSON(t *testing.T) {
	raw := "Here is my review:\n```json\n" +
		`{"findings":[{"rule_id":"R-MONEY-02","file":"internal/x.go","line":12,` +
		`"summary":"float64 for an amount","evidence":"var total float64"}]}` +
		"\n```\n"

	rep, err := review.ParseFindings([]byte(raw), mustRubric(t))
	require.NoError(t, err)
	require.Len(t, rep.Findings, 1)
	assert.Equal(t, "R-MONEY-02", rep.Findings[0].RuleID)
}

func TestParseFindings_FailsClosedOnUnknownRuleID(t *testing.T) {
	// A rule the rubric does not define is an invented one. Accepting it lets the
	// reviewer legislate, and lets a crafted diff name a rule that blocks nothing.
	raw := `{"findings":[{"rule_id":"R-VIBES-99","file":"a.go","line":1,` +
		`"summary":"feels wrong","evidence":"x"}]}`

	_, err := review.ParseFindings([]byte(raw), mustRubric(t))
	require.ErrorContains(t, err, "R-VIBES-99")
}

func TestParseFindings_FailsClosedOnIncompleteFinding(t *testing.T) {
	for name, raw := range map[string]string{
		"no summary": `{"findings":[{"rule_id":"R-SEC-01","file":"a.go","line":1,"evidence":"x"}]}`,
		"no file":    `{"findings":[{"rule_id":"R-SEC-01","line":1,"summary":"s","evidence":"x"}]}`,
		"no rule id": `{"findings":[{"file":"a.go","line":1,"summary":"s","evidence":"x"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := review.ParseFindings([]byte(raw), mustRubric(t))
			require.Error(t, err)
		})
	}
}

func TestParseFindings_SeverityComesFromTheRubricNotTheModel(t *testing.T) {
	// The diff under review is attacker-controlled text and the model reading it
	// is the thing we are asking to judge it. If the model assigns severity, a
	// diff can argue its way below the blocking threshold. The rubric assigns it.
	raw := `{"findings":[{"rule_id":"R-SEC-01","severity":"info","file":"a.go",` +
		`"line":1,"summary":"AWS key literal","evidence":"AKIA..."}]}`

	rep, err := review.ParseFindings([]byte(raw), mustRubric(t))
	require.NoError(t, err)
	require.Len(t, rep.Findings, 1)
	assert.Equal(t, review.Critical, rep.Findings[0].Severity,
		"model-supplied severity must be ignored in favour of the rubric's")
}

func TestBlocking_OnlyAtOrAboveThreshold(t *testing.T) {
	rep := reportWith(t, "R-SEC-01", "R-MONEY-02", "R-STYLE-07")

	tests := []struct {
		threshold review.Severity
		wantRules []string
	}{
		{review.Critical, []string{"R-SEC-01"}},
		{review.High, []string{"R-SEC-01", "R-MONEY-02"}},
		{review.Low, []string{"R-SEC-01", "R-MONEY-02", "R-STYLE-07"}},
	}
	for _, tc := range tests {
		t.Run(tc.threshold.String(), func(t *testing.T) {
			got := review.Blocking(rep, tc.threshold, nil)
			assert.Equal(t, tc.wantRules, ruleIDs(got))
		})
	}
}

func TestBlocking_AcceptedRiskDowngradesButStaysVisible(t *testing.T) {
	// accounted allowed their gate to be ANSWERED, not only obeyed: a finding on
	// a transaction_method backfill was triaged satisfied-by-design with the
	// reasoning recorded. A gate with no such path becomes a rubber stamp or
	// something you route around.
	rep := reportWith(t, "R-SEC-01", "R-MONEY-02")
	accepted := map[string]string{"R-SEC-01": "test fixture, not a live key"}

	blocking := review.Blocking(rep, review.High, accepted)
	assert.Equal(t, []string{"R-MONEY-02"}, ruleIDs(blocking),
		"an accepted rule must stop blocking")

	body := review.RenderComment(rep, blocking, accepted)
	assert.Contains(t, body, "R-SEC-01")
	assert.Contains(t, body, "test fixture, not a live key",
		"the acceptance and its reason stay in the record")
}

func TestBlocking_AcceptanceOfAnUnraisedRuleIsNotAnError(t *testing.T) {
	// A stale acceptance for a finding that no longer fires is harmless; it must
	// not crash the gate or silently mark the PR dirty.
	rep := reportWith(t, "R-MONEY-02")
	blocking := review.Blocking(rep, review.High, map[string]string{"R-SEC-01": "gone now"})
	assert.Equal(t, []string{"R-MONEY-02"}, ruleIDs(blocking))
}

func TestParseAcceptances_ReadsTheDeclaredForm(t *testing.T) {
	comments := []string{
		"looks fine otherwise",
		"review-accept: R-SEC-01 — test fixture, not a live key",
		"review-accept: R-STYLE-07 — internal helper, doc comment adds nothing",
	}
	got := review.ParseAcceptances(comments)

	require.Len(t, got, 2)
	assert.Equal(t, "test fixture, not a live key", got["R-SEC-01"])
}

func TestParseAcceptances_RequiresAReason(t *testing.T) {
	// "review-accept: R-SEC-01" with no reason is a bypass, not a decision.
	got := review.ParseAcceptances([]string{
		"review-accept: R-SEC-01",
		"review-accept: R-SEC-01 —   ",
	})
	assert.Empty(t, got)
}

func TestParseAcceptances_IgnoresAQuotedAcceptance(t *testing.T) {
	// The reviewer's own sticky comment quotes finding text back. If that text
	// were parsed as an acceptance, the gate could disarm itself on the next run.
	got := review.ParseAcceptances([]string{
		"> review-accept: R-SEC-01 — quoted from someone else",
		"    review-accept: R-MONEY-02 — indented code block",
	})
	assert.Empty(t, got)
}

func TestRenderComment_IsStickyAndStatesTheVerdict(t *testing.T) {
	rep := reportWith(t, "R-SEC-01")
	blocking := review.Blocking(rep, review.High, nil)

	body := review.RenderComment(rep, blocking, nil)

	assert.True(t, strings.HasPrefix(body, review.StickyMarker),
		"the marker must lead so the workflow can find and update one comment")
	assert.Contains(t, body, "BLOCKED")
	assert.Contains(t, body, "R-SEC-01")
	assert.Contains(t, body, "review-accept:", "the answer path must be discoverable from the comment")
}

func TestRenderComment_CleanReviewSaysSoExplicitly(t *testing.T) {
	rep, err := review.ParseFindings([]byte(`{"findings": []}`), mustRubric(t))
	require.NoError(t, err)

	body := review.RenderComment(rep, nil, nil)
	assert.True(t, strings.HasPrefix(body, review.StickyMarker))
	assert.Contains(t, body, "No findings")
}

// --- helpers ---

func mustRubric(t *testing.T) review.Rubric {
	t.Helper()
	r, err := review.ParseRubric([]byte(testRubric))
	require.NoError(t, err)
	return r
}

func reportWith(t *testing.T, ruleIDs ...string) review.Report {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(`{"findings":[`)
	for i, id := range ruleIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"rule_id":"` + id + `","file":"internal/x.go","line":` +
			"1" + `,"summary":"summary for ` + id + `","evidence":"evidence"}`)
	}
	sb.WriteString(`]}`)

	rep, err := review.ParseFindings([]byte(sb.String()), mustRubric(t))
	require.NoError(t, err)
	return rep
}

func ruleIDs(fs []review.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.RuleID)
	}
	return out
}
