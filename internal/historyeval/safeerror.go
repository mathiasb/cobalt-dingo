package historyeval

import (
	"regexp"
	"strings"
)

var statusRe = regexp.MustCompile(`unexpected status (\d{3})`)

// SafeError names the CLASS of a failure without any of its text. Errors out
// of the Fortnox client wrap request URLs (which hold an invoice number) and
// the raw value that failed to decode (an amount, a name); the company check
// names the company the token opens. None of that may reach a Job log, which
// agent sessions read and a brain wiki ingests (#94).
func SafeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "token opens") {
		return "company check failed: the token opens a different company than the one selected"
	}
	if m := statusRe.FindStringSubmatch(msg); m != nil {
		return "Fortnox returned HTTP " + m[1]
	}
	for _, w := range []string{"decode", "unmarshal", "not a number", "neither a string nor a number", "invalid character"} {
		if strings.Contains(msg, w) {
			return "a Fortnox response could not be decoded"
		}
	}
	if strings.Contains(msg, "redirects") {
		return "Fortnox answered with a redirect loop"
	}
	for _, w := range []string{"dial tcp", "no such host", "connection refused", "timeout", "deadline exceeded", "EOF"} {
		if strings.Contains(msg, w) {
			return "network error talking to Fortnox"
		}
	}
	return "unclassified error (details withheld)"
}
