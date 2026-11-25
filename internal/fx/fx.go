// Package fx holds published exchange rates and converts amounts to SEK with them.
//
// Rates come from a local file, made by scripts/fetch-riksbank-rates.py from
// Riksbanken's published daily rates. Nothing here makes a network call: the
// demo's bet guardrail is that no document or booking data leaves the local
// network, and a lookup keyed on an invoice's currency and date is a small leak
// that a file avoids.
//
// A rate is an exact integer of micro-SEK per one unit of the currency, never a
// float, and conversion is big-integer arithmetic rounded half away from zero.
// The rate is data with a source and a date; no model chooses it.
package fx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// MaxFileBytes bounds the rate file. A year of six currencies is ~60 KB.
	MaxFileBytes = 1 << 20
	// MaxObservations bounds the rates in the file in total.
	MaxObservations = 20000

	maxSourceLen = 120
	maxLookback  = 7 * 24 * time.Hour

	// microPerUnit: a rate is stored as millionths of a krona per unit.
	microPerUnit = 1_000_000
	minMicro     = 100             // 0.0001 SEK per unit
	maxMicro     = 100_000_000_000 // 100000 SEK per unit

	// maxConvertible is the largest amount, in minor units, converted: the same
	// bound as extract.MaxAmountMinor, kept here so the package stands alone.
	maxConvertible = 1_000_000_000_000
)

// ErrNoRate means no rate could be found; the message says why and is safe to show.
var ErrNoRate = errors.New("no exchange rate")

// Rate is one currency's rate for one day.
type Rate struct {
	Currency string
	Date     string // the day the rate was published for; may be earlier than the invoice date
	Micro    int64  // millionths of a SEK per one unit of Currency
	Source   string
}

type obs struct {
	date  time.Time
	iso   string
	micro int64
}

// Table is a validated set of rates.
type Table struct {
	source string
	byCur  map[string][]obs // ascending by date
}

type file struct {
	Source     string            `json:"source"`
	Fetched    string            `json:"fetched"`
	Currencies map[string][]item `json:"currencies"`
}

type item struct {
	Date string `json:"date"`
	Rate string `json:"rate"`
}

var (
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	rateRe     = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,6})?$`)
)

// Load reads and validates a rate file. Any problem refuses the whole file: a
// half-trusted table would be worse than none.
func Load(r io.Reader) (*Table, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read rate file: %w", err)
	}
	if len(raw) > MaxFileBytes {
		return nil, fmt.Errorf("rate file is larger than %d bytes", MaxFileBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("rate file is not valid json for this format: %w", err)
	}
	if f.Source == "" || len(f.Source) > maxSourceLen {
		return nil, fmt.Errorf("rate file source must be 1 to %d characters", maxSourceLen)
	}
	if len(f.Currencies) == 0 {
		return nil, errors.New("rate file has no rates")
	}
	total := 0
	for _, items := range f.Currencies {
		total += len(items)
	}
	if total > MaxObservations {
		return nil, fmt.Errorf("rate file has too many rates (more than %d)", MaxObservations)
	}

	t := &Table{source: f.Source, byCur: map[string][]obs{}}
	for cur, items := range f.Currencies {
		if !currencyRe.MatchString(cur) {
			return nil, fmt.Errorf("rate file currency %q is not a three-letter code", cur)
		}
		if cur == "SEK" {
			return nil, errors.New("rate file must not contain SEK: it is the posting currency")
		}
		var prev time.Time
		list := make([]obs, 0, len(items))
		for _, it := range items {
			d, err := time.Parse("2006-01-02", it.Date)
			if err != nil || d.Format("2006-01-02") != it.Date {
				return nil, fmt.Errorf("rate file %s: %q is not a YYYY-MM-DD date", cur, it.Date)
			}
			if !prev.IsZero() && !d.After(prev) {
				return nil, fmt.Errorf("rate file %s: dates must be in strictly ascending order, %s is not after the one before", cur, it.Date)
			}
			prev = d
			m, err := parseMicro(it.Rate)
			if err != nil {
				return nil, fmt.Errorf("rate file %s %s: rate %q: %w", cur, it.Date, it.Rate, err)
			}
			list = append(list, obs{date: d, iso: it.Date, micro: m})
		}
		if len(list) == 0 {
			continue
		}
		t.byCur[cur] = list
	}
	if len(t.byCur) == 0 {
		return nil, errors.New("rate file has no rates")
	}
	return t, nil
}

// parseMicro reads a plain decimal with up to six places into millionths,
// without floating point, and checks it is a plausible rate.
func parseMicro(s string) (int64, error) {
	if !rateRe.MatchString(s) {
		return 0, errors.New("is not a plain decimal with at most six places")
	}
	whole, frac, _ := strings.Cut(s, ".")
	for len(frac) < 6 {
		frac += "0"
	}
	var n int64
	for _, c := range whole + frac {
		n = n*10 + int64(c-'0')
	}
	if n < minMicro || n > maxMicro {
		return 0, errors.New("is outside the plausible range 0.0001 to 100000 SEK per unit")
	}
	return n, nil
}

// Currencies lists the currencies in the table, sorted.
func (t *Table) Currencies() []string {
	out := make([]string, 0, len(t.byCur))
	for c := range t.byCur {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the rate for a currency on a date: that day's, or the latest
// earlier one within seven days (weekends and holidays have no published rate).
// Otherwise it returns ErrNoRate, with a reason that is safe to show.
func (t *Table) Lookup(currency, date string) (Rate, error) {
	if !currencyRe.MatchString(currency) {
		return Rate{}, fmt.Errorf("%w: %q is not a currency code", ErrNoRate, currency)
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil || d.Format("2006-01-02") != date {
		return Rate{}, fmt.Errorf("%w: the invoice date %q is not a usable date", ErrNoRate, date)
	}
	list, ok := t.byCur[currency]
	if !ok {
		return Rate{}, fmt.Errorf("%w for %s: the rate file has no %s", ErrNoRate, currency, currency)
	}
	// The latest observation on or before d.
	i := sort.Search(len(list), func(i int) bool { return list[i].date.After(d) }) - 1
	if i < 0 || d.Sub(list[i].date) > maxLookback {
		return Rate{}, fmt.Errorf("%w for %s on %s: none published within the 7 days before it", ErrNoRate, currency, date)
	}
	o := list[i]
	return Rate{Currency: currency, Date: o.iso, Micro: o.micro, Source: t.source}, nil
}

// String is the rate as a decimal with at least four places and no needless zeros.
func (r Rate) String() string {
	s := fmt.Sprintf("%d.%06d", r.Micro/microPerUnit, r.Micro%microPerUnit)
	for strings.HasSuffix(s, "0") && len(s)-strings.IndexByte(s, '.') > 5 {
		s = s[:len(s)-1]
	}
	return s
}

// Convert returns the amount, in minor units of the currency, as minor units of
// SEK, rounded half away from zero. A negative amount is refused (credit notes
// are not converted), and so is one too large to convert exactly.
func (r Rate) Convert(minor int64) (int64, error) {
	if minor < 0 {
		return 0, errors.New("a negative amount is not converted")
	}
	if minor > maxConvertible {
		return 0, fmt.Errorf("an amount above %d minor units is not converted", maxConvertible)
	}
	if r.Micro < minMicro || r.Micro > maxMicro {
		return 0, errors.New("the rate is outside the accepted range")
	}
	p := new(big.Int).Mul(big.NewInt(minor), big.NewInt(r.Micro))
	half := big.NewInt(microPerUnit / 2)
	p.Add(p, half)
	p.Quo(p, big.NewInt(microPerUnit)) // non-negative, so truncation rounds half up = half away from zero
	if !p.IsInt64() {
		return 0, errors.New("the converted amount is too large")
	}
	return p.Int64(), nil
}
