package receipts

import "strings"

// Destination describes where matched mail should be forwarded.
// Type is one of: "smtp", "fortnox-receipts", "fortnox-invoices".
type Destination struct {
	Name    string
	Type    string
	Address string
}

// Rule maps matching criteria to a named destination.
type Rule struct {
	Name         string
	MatchFrom    []string
	MatchSubject []string
	Destination  string
}

// Router evaluates rules in order and returns the first matching destination.
type Router struct {
	rules        []Rule
	destinations map[string]*Destination
}

// NewRouter builds a Router from a slice of rules and destinations.
func NewRouter(rules []Rule, destinations []*Destination) *Router {
	dm := make(map[string]*Destination, len(destinations))
	for _, d := range destinations {
		dm[d.Name] = d
	}
	return &Router{rules: rules, destinations: dm}
}

// Route returns the destination for m, or (nil, false) if no rule matches.
func (r *Router) Route(m Mail) (*Destination, bool) {
	for _, rule := range r.rules {
		if ruleMatches(rule, m) {
			d, ok := r.destinations[rule.Destination]
			return d, ok
		}
	}
	return nil, false
}

// ruleMatches reports whether m satisfies every criterion the rule names.
//
// Criteria are ANDed; alternatives WITHIN a criterion are ORed. So a rule
// naming both a sender and a subject means "from this sender AND about this",
// which is how the routing config is written:
//
//   - name: hetzner-invoice
//     match_from:    ["*@hetzner.com"]
//     match_subject: ["Invoice"]
//
// A criterion left empty is simply not a constraint, so single-criterion rules
// behave exactly as before.
//
// This was previously an OR across both criteria, which made the sender
// constraint decorative: any mail with "Invoice" in the subject, from anyone,
// routed to Fortnox as a supplier invoice. Nothing caught it because every
// existing rule in the tests named only one criterion.
func ruleMatches(rule Rule, m Mail) bool {
	if len(rule.MatchFrom) == 0 && len(rule.MatchSubject) == 0 {
		return false // a rule that constrains nothing must not match everything
	}
	return matchesAnyAddress(m.From, rule.MatchFrom) &&
		containsAnySubject(m.Subject, rule.MatchSubject)
}

// matchesAnyAddress reports whether addr matches any pattern. No patterns means
// the rule does not constrain the sender.
func matchesAnyAddress(addr string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if matchesAddress(addr, p) {
			return true
		}
	}
	return false
}

// containsAnySubject reports whether subject contains any pattern,
// case-insensitively. No patterns means the rule does not constrain the subject.
func containsAnySubject(subject string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	lower := strings.ToLower(subject)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// matchesAddress checks an exact address or a wildcard domain pattern (*@domain).
func matchesAddress(addr, pattern string) bool {
	if strings.EqualFold(addr, pattern) {
		return true
	}
	if strings.HasPrefix(pattern, "*@") {
		domain := strings.TrimPrefix(pattern, "*@")
		return strings.HasSuffix(strings.ToLower(addr), "@"+strings.ToLower(domain))
	}
	return false
}
