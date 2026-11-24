package demo

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// cgnat is 100.64.0.0/10, the range Tailscale assigns.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// ValidateModelURL refuses a model endpoint that is not on this network. The
// bet's guardrail is that no document or booking data leaves the local network,
// and a mistyped or copied cloud URL would break it without any error. Allowed:
// loopback, private ranges, the Tailscale range, a single-label host name such
// as "iguana", and *.ts.net.
func ValidateModelURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("model URL %q is not a valid http(s) URL", raw)
	}
	host := u.Hostname()
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.IsLoopback() || addr.IsPrivate() || cgnat.Contains(addr) {
			return nil
		}
		return fmt.Errorf("model address %s is not on the local network", host)
	}
	if host == "localhost" || !strings.Contains(host, ".") || strings.HasSuffix(host, ".ts.net") {
		return nil
	}
	return fmt.Errorf("model host %q is not a local name: documents must not leave the local network", host)
}
