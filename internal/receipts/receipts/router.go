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

func ruleMatches(rule Rule, m Mail) bool {
	for _, pattern := range rule.MatchFrom {
		if matchesAddress(m.From, pattern) {
			return true
		}
	}
	for _, pattern := range rule.MatchSubject {
		if strings.Contains(strings.ToLower(m.Subject), strings.ToLower(pattern)) {
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
