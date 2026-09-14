// Package apishape reports the shape of a live Fortnox response and compares
// it with the fields our structs actually read.
//
// It exists because two production defects came from the same cause: a field
// present in one and absent in the other, with no error either way. Go's
// json.Unmarshal ignores unknown keys AND leaves absent keys at their zero
// value, so `TotalInvoiceCurrency float64` reading a response that has no such
// field yields 0.00 — a plausible amount for an invoice (#92).
//
// The vendored OpenAPI spec is a document, not the API. This reads the API.
package apishape

import (
	"encoding/json"
	"fmt"
	"sort"
)

// RecordKeys returns the sorted JSON keys of the first record in a named
// collection.
func RecordKeys(body []byte, collection string) ([]string, error) {
	types, err := RecordTypes(body, collection)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(types))
	for k := range types {
		keys = append(keys, k)
	}
	// Sorted so two runs of the same probe are diffable; map order would make
	// an unchanged API look like a change.
	sort.Strings(keys)
	return keys, nil
}

// RecordTypes returns the JSON type of each key in the first record.
//
// Types are half the point: a float64 field silently decodes a JSON string to
// zero, which is how an invoice total becomes 0.00 without an error anywhere.
func RecordTypes(body []byte, collection string) (map[string]string, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	raw, ok := envelope[collection]
	if !ok {
		present := make([]string, 0, len(envelope))
		for k := range envelope {
			present = append(present, k)
		}
		sort.Strings(present)
		return nil, fmt.Errorf("no collection %q in response; found %v", collection, present)
	}

	var records []map[string]any
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("decode collection %q: %w", collection, err)
	}
	if len(records) == 0 {
		// An empty collection cannot describe a shape. Returning an empty key
		// set would read as "this record has no fields", which is the kind of
		// confident nothing this package exists to stop.
		return nil, fmt.Errorf("collection %q contains no records, so it cannot describe a shape", collection)
	}

	types := make(map[string]string, len(records[0]))
	for k, v := range records[0] {
		types[k] = jsonType(v)
	}
	return types, nil
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// Divergence is the two-way difference between what the API sends and what we
// read.
type Divergence struct {
	// LiveNotRead is sent by Fortnox and ignored by us. Usually fine,
	// occasionally a flag like Removed that changes what a record means.
	LiveNotRead []string

	// ReadNotLive is decoded by us and NOT sent. These are the dangerous ones:
	// each is silently the zero value of its type.
	ReadNotLive []string
}

// Compare returns the two-way difference between live keys and the json tags
// our struct reads.
func Compare(liveKeys, ourFields []string) Divergence {
	live := set(liveKeys)
	ours := set(ourFields)

	var d Divergence
	for _, k := range liveKeys {
		if !ours[k] {
			d.LiveNotRead = append(d.LiveNotRead, k)
		}
	}
	for _, k := range ourFields {
		if !live[k] {
			d.ReadNotLive = append(d.ReadNotLive, k)
		}
	}
	sort.Strings(d.LiveNotRead)
	sort.Strings(d.ReadNotLive)
	return d
}

func set(keys []string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}
