package fortnox

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// FlexString is a string that Fortnox may send quoted or as a bare number.
//
// The third Flex type for one vendor quirk, and deliberately so: FlexInt
// exists because Fortnox varies numeric types across API versions, FlexFloat
// because it varies them across ENDPOINTS, and this one because a supplier's
// own invoice reference arrives as a string live ("2026/A-114") but is a field
// a supplier could plausibly fill with digits.
//
// Tolerant rather than strict here on purpose. This field is not an identity —
// Fortnox's GivenNumber is — so failing an entire payables fetch because one
// supplier's reference arrived as a number would be disproportionate.
type FlexString string

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("value is neither a string nor a number: %s", data)
	}
	// json.Number preserves the literal, so an integer does not acquire a
	// decimal point on the way through.
	if _, err := strconv.ParseFloat(n.String(), 64); err != nil {
		return fmt.Errorf("value %q is not a usable scalar: %w", n.String(), err)
	}
	*f = FlexString(n.String())
	return nil
}
