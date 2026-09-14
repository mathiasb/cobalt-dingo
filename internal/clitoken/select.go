// Package clitoken resolves which connected company a command-line tool should
// read, and loads its stored token.
//
// Extracted rather than copied: cmd/fortnox-check had this inline and
// discarded the tenant ID, which the voucher cache needs as its key. A third
// copy was the point to stop.
package clitoken

import (
	"fmt"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// SelectTenant picks the connected company for a mode.
//
// company disambiguates when more than one is connected in that mode; empty
// means "the only one". It fails closed in both directions — no match and
// several matches are both errors — because the alternative is running against
// books nobody named. Two companies on this account share organisation number
// 556836-0688 (#87), so neither the tenant ID nor the name is a reliable
// discriminator on its own, and an arbitrary pick would look like a choice.
func SelectTenant(tenants []domain.Tenant, mode, company string) (domain.Tenant, error) {
	suffix := ":" + mode + ":"

	var matches []domain.Tenant
	for _, t := range tenants {
		if strings.Contains(string(t.ID), suffix) {
			matches = append(matches, t)
		}
	}
	if len(matches) == 0 {
		return domain.Tenant{}, fmt.Errorf("no company connected for mode %s — connect one in the web UI first", mode)
	}

	if want := strings.TrimSpace(company); want != "" {
		for _, t := range matches {
			if strings.EqualFold(strings.TrimSpace(t.Name), want) {
				return t, nil
			}
		}
		return domain.Tenant{}, fmt.Errorf("company %q is not connected for mode %s — refusing to fall back to another", want, mode)
	}

	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, t := range matches {
			names[i] = t.Name
		}
		return domain.Tenant{}, fmt.Errorf(
			"%d companies are connected for mode %s (%s) — set FORTNOX_COMPANY to name one; refusing to guess",
			len(matches), mode, strings.Join(names, ", "))
	}
	return matches[0], nil
}
