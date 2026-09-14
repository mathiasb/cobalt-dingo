package fortnox

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexFloat is a float that Fortnox may send as a bare number or a quoted
// string.
//
// Necessary because Fortnox is inconsistent ACROSS endpoints, not only across
// versions: /3/supplierinvoices sends Total and Balance as strings while
// /3/invoices sends them as numbers (verified live 2026-09-14 with
// cmd/fortnox-shape). A plain float64 field decodes a JSON string to zero
// WITHOUT erroring, and zero is a plausible invoice amount — which is how
// every supplier invoice in this system read SEK 0.00.
//
// An unparseable value is an error, not zero. Swallowing it would reintroduce
// the exact defect.
type FlexFloat float64

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexFloat) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = 0
		return nil
	}

	var n float64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexFloat(n)
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("amount is neither a number nor a string: %s", data)
	}
	if s == "" {
		*f = 0
		return nil
	}
	parsed, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("amount %q is not a number: %w", s, err)
	}
	*f = FlexFloat(parsed)
	return nil
}
