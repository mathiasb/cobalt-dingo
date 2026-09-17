// Package review turns a model's reading of a pull-request diff into a verdict
// the CI gate can act on.
//
// Everything here is deliberately pure: the workflow does the I/O — fetch the
// diff, call the model, post the comment — and this package decides what the
// answer means. That split exists so the part where correctness lives is
// testable, because the part that talks to a model is not.
//
// Two properties carry the design:
//
//   - Fail closed. A model that returns nothing, returns prose, or names a rule
//     the rubric does not define is a broken reviewer, not a clean diff. Only an
//     explicit empty findings list means "reviewed, found nothing".
//   - Severity comes from the rubric, never from the model. The diff is
//     attacker-controlled text and the model reading it is the thing being asked
//     to judge it; if the model graded its own findings, a diff could argue its
//     way below the blocking threshold.
package review

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// StickyMarker leads the review comment so the workflow can find the one comment
// it owns and update it, rather than appending a new one per run.
const StickyMarker = "<!-- cobalt-dingo:pr-review:v1 -->"

// Severity orders findings against the gate's blocking threshold.
type Severity int

// Severity levels, ascending. The zero value is Info so an unset severity cannot
// accidentally block.
const (
	Info Severity = iota
	Low
	Medium
	High
	Critical
)

var severityNames = map[Severity]string{
	Info: "info", Low: "low", Medium: "medium", High: "high", Critical: "critical",
}

func (s Severity) String() string {
	if n, ok := severityNames[s]; ok {
		return n
	}
	return fmt.Sprintf("Severity(%d)", int(s))
}

// ParseSeverity reads a rubric severity word.
func ParseSeverity(s string) (Severity, error) {
	for sev, name := range severityNames {
		if name == strings.ToLower(strings.TrimSpace(s)) {
			return sev, nil
		}
	}
	return Info, fmt.Errorf("unknown severity %q", s)
}

// Rule is one rubric entry: what it is called, how much it matters, and what it
// means.
type Rule struct {
	ID       string
	Severity Severity
	Title    string
}

// Rubric is the set of rules a diff may be found to violate. A finding naming
// anything outside it is rejected.
type Rubric struct {
	Rules map[string]Rule
}

var (
	ruleHeading = regexp.MustCompile(`^##\s+(\S+)\s+—\s+([^—]+?)\s+—\s+(.+?)\s*$`)
	ruleID      = regexp.MustCompile(`^R-[A-Z]+-\d{2}$`)
)

// ParseRubric reads the rubric markdown. A `##` heading containing an em-dash
// separator is taken to be a rule and validated strictly: a malformed rule is an
// error rather than a silently skipped line, because a rule that fails to load is
// a check that silently stops running.
func ParseRubric(raw []byte) (Rubric, error) {
	r := Rubric{Rules: map[string]Rule{}}

	for i, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "## ") || !strings.Contains(line, " — ") {
			continue
		}
		m := ruleHeading.FindStringSubmatch(line)
		if m == nil {
			return Rubric{}, fmt.Errorf("rubric line %d: malformed rule heading: %s", i+1, line)
		}
		id, sevWord, title := m[1], m[2], m[3]

		if !ruleID.MatchString(id) {
			return Rubric{}, fmt.Errorf("rubric line %d: %q is not a rule id (want R-DOMAIN-NN)", i+1, id)
		}
		sev, err := ParseSeverity(sevWord)
		if err != nil {
			return Rubric{}, fmt.Errorf("rubric line %d: rule %s: %w", i+1, id, err)
		}
		if _, dup := r.Rules[id]; dup {
			return Rubric{}, fmt.Errorf("rubric line %d: rule %s is defined twice", i+1, id)
		}
		r.Rules[id] = Rule{ID: id, Severity: sev, Title: title}
	}

	if len(r.Rules) == 0 {
		// A rubric with no rules would make every review vacuously clean: the
		// "no output passes as green" failure wearing a different costume.
		return Rubric{}, fmt.Errorf("rubric defines no rules")
	}
	return r, nil
}

// Finding is one rule violation the model located. Severity and Title are filled
// from the rubric on parse; any values the model supplied for them are discarded.
type Finding struct {
	RuleID   string `json:"rule_id"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Summary  string `json:"summary"`
	Evidence string `json:"evidence"`

	Severity Severity `json:"-"`
	Title    string   `json:"-"`
}

// Report is a completed review. Reviewed distinguishes "found nothing" from
// "produced nothing", which is the distinction the gate rests on.
type Report struct {
	Findings []Finding
	Reviewed bool
}

type wireReport struct {
	Findings *[]Finding `json:"findings"`
}

var jsonObject = regexp.MustCompile(`(?s)\{.*\}`)

// ParseFindings reads the model's answer and grades it against the rubric.
//
// It tolerates a JSON object wrapped in prose or a fenced code block, because
// models emit that routinely and rejecting it would be brittleness rather than
// strictness. It does not tolerate a missing findings list, a finding without a
// location or summary, or a rule the rubric does not define.
func ParseFindings(raw []byte, rubric Rubric) (Report, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Report{}, fmt.Errorf("model produced no output: the reviewer did not run, which is not the same as a clean diff")
	}

	obj := jsonObject.Find(raw)
	if obj == nil {
		return Report{}, fmt.Errorf("model output contains no JSON object: %.200q", raw)
	}

	var wire wireReport
	if err := json.Unmarshal(obj, &wire); err != nil {
		return Report{}, fmt.Errorf("decode model output: %w", err)
	}
	if wire.Findings == nil {
		return Report{}, fmt.Errorf(`model output has no "findings" key; an empty review must say {"findings": []}`)
	}

	out := Report{Reviewed: true, Findings: make([]Finding, 0, len(*wire.Findings))}
	for i, f := range *wire.Findings {
		if f.RuleID == "" {
			return Report{}, fmt.Errorf("finding %d: no rule_id", i)
		}
		rule, ok := rubric.Rules[f.RuleID]
		if !ok {
			return Report{}, fmt.Errorf("finding %d names rule %s, which the rubric does not define", i, f.RuleID)
		}
		if strings.TrimSpace(f.File) == "" {
			return Report{}, fmt.Errorf("finding %d (%s): no file", i, f.RuleID)
		}
		if strings.TrimSpace(f.Summary) == "" {
			return Report{}, fmt.Errorf("finding %d (%s): no summary", i, f.RuleID)
		}
		f.Severity = rule.Severity
		f.Title = rule.Title
		out.Findings = append(out.Findings, f)
	}
	return out, nil
}

// Blocking returns the findings that should fail the gate: those at or above the
// threshold whose rule has not been accepted on the PR. Input order is preserved
// so the comment reads in the order the reviewer found things.
func Blocking(rep Report, threshold Severity, accepted map[string]string) []Finding {
	var out []Finding
	for _, f := range rep.Findings {
		if f.Severity < threshold {
			continue
		}
		if _, ok := accepted[f.RuleID]; ok {
			continue
		}
		out = append(out, f)
	}
	return out
}

// An acceptance must carry a reason. `--` is allowed alongside the em-dash
// because the em-dash is awkward to type on a phone, which is where these get
// written.
var acceptance = regexp.MustCompile(`^review-accept:\s*(R-[A-Z]+-\d{2})\s*(?:—|--)\s*(\S.*?)\s*$`)

// ParseAcceptances reads `review-accept: R-XXX-NN — reason` lines out of PR
// comments.
//
// Quoted and indented lines are ignored on purpose: the reviewer's own sticky
// comment quotes acceptances back, and parsing its own output would let the gate
// disarm itself on the next run.
func ParseAcceptances(comments []string) map[string]string {
	out := map[string]string{}
	for _, c := range comments {
		for _, line := range strings.Split(c, "\n") {
			line = strings.TrimRight(line, " \t\r")
			if line != strings.TrimLeft(line, " \t>") {
				continue
			}
			if m := acceptance.FindStringSubmatch(line); m != nil {
				out[m[1]] = m[2]
			}
		}
	}
	return out
}

// RenderComment builds the sticky review comment. It states the verdict first,
// then every finding, then the acceptances in force — so the record shows what
// was raised and answered, not only what still blocks.
func RenderComment(rep Report, blocking []Finding, accepted map[string]string) string {
	var b strings.Builder
	b.WriteString(StickyMarker)
	b.WriteString("\n## Review\n\n")

	switch {
	case len(blocking) > 0:
		fmt.Fprintf(&b, "**BLOCKED** — %s at or above the threshold.\n\n", plural(len(blocking), "finding"))
	case len(rep.Findings) > 0:
		b.WriteString("**PASS** — findings below the blocking threshold, or accepted.\n\n")
	default:
		b.WriteString("**PASS** — No findings.\n\n")
	}

	if len(rep.Findings) > 0 {
		b.WriteString("| Rule | Severity | Where | Finding |\n|---|---|---|---|\n")
		for _, f := range rep.Findings {
			status := ""
			if _, ok := accepted[f.RuleID]; ok {
				status = " *(accepted)*"
			}
			fmt.Fprintf(&b, "| `%s`%s | %s | `%s:%d` | %s |\n",
				f.RuleID, status, f.Severity, f.File, f.Line, oneLine(f.Summary))
		}
		b.WriteString("\n")
	}

	if len(accepted) > 0 {
		b.WriteString("### Accepted on this PR\n\n")
		for _, id := range sortedKeys(accepted) {
			fmt.Fprintf(&b, "- `%s` — %s\n", id, oneLine(accepted[id]))
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n\n")
	b.WriteString("Rules live in [`.review/rubric.md`](../blob/main/.review/rubric.md), " +
		"and severity comes from there rather than from the model.\n\n")
	b.WriteString("To answer a finding rather than fix it, comment:\n\n" +
		"    review-accept: R-XXX-NN -- why this is fine here\n\n" +
		"The reason is mandatory and stays in this comment as part of the record.\n")

	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// oneLine keeps model-authored text from breaking the markdown table it sits in.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.TrimSpace(s)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
