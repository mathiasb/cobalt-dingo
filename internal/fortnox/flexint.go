package fortnox

import (
	"encoding/json"
	"strconv"
)

// FlexInt unmarshals a JSON value that Fortnox may send as either a bare number
// or a quoted string (and sometimes an empty string meaning zero).
type FlexInt int

// UnmarshalJSON handles Fortnox responses that send ints as bare numbers or quoted strings.
func (f *FlexInt) UnmarshalJSON(data []byte) error {
	// Try bare number first.
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}
	// Fall back to quoted string.
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f = FlexInt(n)
	return nil
}
