package fortnox

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// totalResourcesEnvelope reads only the count from a Fortnox list response.
//
// Both levels are POINTERS on purpose. Decoded into plain ints, a response
// that omits MetaInformation — an error page, a changed API shape, a proxy
// returning something else — yields 0, which is indistinguishable from a
// genuinely empty result set. For "how many unpaid invoices are there?" those
// two answers lead to opposite conclusions about whether the company owes
// anyone money.
type totalResourcesEnvelope struct {
	Meta *struct {
		TotalResources *int `json:"@TotalResources"`
	} `json:"MetaInformation"`
}

// TotalResources returns the record count a Fortnox list endpoint reports,
// without fetching the records.
//
// One request, whatever the collection size, which is what makes it usable as
// an integrity check rather than an alternative to the fetch.
//
// Zero is a valid answer. Absent is an error — see totalResourcesEnvelope.
func (c *Client) TotalResources(requestURL string) (int, error) {
	raw, err := c.get(requestURL)
	if err != nil {
		return 0, fmt.Errorf("count records: %w", err)
	}
	var env totalResourcesEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return 0, fmt.Errorf("decode record count: %w", err)
	}
	if env.Meta == nil || env.Meta.TotalResources == nil {
		return 0, fmt.Errorf(
			"response carried no MetaInformation.@TotalResources — refusing to report 0, which would be indistinguishable from an empty result")
	}
	return *env.Meta.TotalResources, nil
}

// SupplierInvoiceFilters and CustomerInvoiceFilters are every status filter
// Fortnox documents for each endpoint, taken from the vendored OpenAPI spec.
//
// They exist as lists because a single "unpaid" count cannot distinguish a
// settled company from one whose invoices are all UNBOOKED — `unbooked` is a
// separate filter value, not a subset of `unpaid`. Supplier invoices also have
// pendingpayment and authorizepending, which customer invoices do not.
var (
	SupplierInvoiceFilters = []string{
		"unpaid", "unpaidoverdue", "unbooked", "pendingpayment",
		"authorizepending", "fullypaid", "cancelled",
	}
	CustomerInvoiceFilters = []string{
		"unpaid", "unpaidoverdue", "unbooked", "fullypaid", "cancelled",
	}
)

// SupplierInvoiceStateCounts counts supplier invoices under every documented
// filter. One request per filter, none of which fetches an invoice.
func (c *Client) SupplierInvoiceStateCounts() (map[string]int, error) {
	return c.invoiceStateCounts("/3/supplierinvoices", SupplierInvoiceFilters)
}

// CustomerInvoiceStateCounts counts customer invoices under every documented
// filter.
func (c *Client) CustomerInvoiceStateCounts() (map[string]int, error) {
	return c.invoiceStateCounts("/3/invoices", CustomerInvoiceFilters)
}

// invoiceStateCounts returns one count per filter, or an error.
//
// A filter that cannot be counted fails the whole call rather than being
// omitted or recorded as zero. A partial map would understate obligations,
// and understating what the company owes is the one direction that matters.
func (c *Client) invoiceStateCounts(path string, filters []string) (map[string]int, error) {
	counts := make(map[string]int, len(filters))
	for _, f := range filters {
		n, err := c.TotalResources(fmt.Sprintf("%s%s?filter=%s", c.baseURL, path, url.QueryEscape(f)))
		if err != nil {
			return nil, fmt.Errorf("count %s filter=%s: %w", path, f, err)
		}
		counts[f] = n
	}
	return counts, nil
}
