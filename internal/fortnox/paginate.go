package fortnox

import (
	"encoding/json"
	"fmt"
)

// listAll fetches every page of a Fortnox collection and concatenates the
// decoded items.
//
// Generic so the page loop exists once. Ten hand-written copies of it is how
// three of them ended up missing (#90): the voucher list returned exactly 100
// of 405 vouchers, ListAccounts had the same gap, and both unpaid-invoice
// fetches read page one only. Every time GetAllPages already existed and the
// call site simply did not use it.
//
// items receives one page's envelope and returns that page's records, so each
// endpoint keeps its own envelope shape — Fortnox names the collection
// differently on every one.
func listAll[T any](c *Client, requestURL, what string, items func(json.RawMessage) ([]T, error)) ([]T, error) {
	pages, err := c.GetAllPages(requestURL)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", what, err)
	}
	var out []T
	for i, raw := range pages {
		page, err := items(raw)
		if err != nil {
			return nil, fmt.Errorf("decode %s page %d: %w", what, i+1, err)
		}
		out = append(out, page...)
	}
	return out, nil
}

// decodeInto is the common case: unmarshal a page into an envelope and pull one
// field out of it.
func decodeInto[E any, T any](raw json.RawMessage, pick func(E) []T) ([]T, error) {
	var envelope E
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	return pick(envelope), nil
}
