package extract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DateOrder says how a numeric date with the day and month both 12 or below
// is read: 02.11.2026 is 2 November under DMY and 11 February under MDY.
type DateOrder int

// Date orders.
const (
	DMY DateOrder = iota // day, month, year: most of Europe, including Sweden
	MDY                  // month, day, year: the United States
)

// Date is a date read from the text of an invoice.
type Date struct {
	ISO string // YYYY-MM-DD
	// Ambiguous is true when the text allowed two different valid dates and
	// the order setting chose one. A booking into the wrong period is silent,
	// so the caller must surface this rather than trust the choice.
	Ambiguous bool
}

var (
	isoRe     = regexp.MustCompile(`^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$`)
	numericRe = regexp.MustCompile(`^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4}|\d{2})$`)
	dayNameRe = regexp.MustCompile(`^(\d{1,2})\.?\s+([A-Za-zåäöÅÄÖ]+)\.?,?\s+(\d{4})$`)
	nameDayRe = regexp.MustCompile(`^([A-Za-zåäöÅÄÖ]+)\.?\s+(\d{1,2}),?\s+(\d{4})$`)
)

var months = map[string]int{
	"jan": 1, "january": 1, "januari": 1,
	"feb": 2, "february": 2, "februari": 2,
	"mar": 3, "march": 3, "mars": 3,
	"apr": 4, "april": 4,
	"may": 5, "maj": 5,
	"jun": 6, "june": 6, "juni": 6,
	"jul": 7, "july": 7, "juli": 7,
	"aug": 8, "august": 8, "augusti": 8,
	"sep": 9, "sept": 9, "september": 9,
	"oct": 10, "okt": 10, "october": 10, "oktober": 10,
	"nov": 11, "november": 11,
	"dec": 12, "december": 12,
}

// ParseDate reads a date as printed on an invoice. The model is asked for the
// text unchanged and this function, not the model, decides what it means, so
// the day/month order is an explicit setting and not a guess made per image.
func ParseDate(text string, order DateOrder) (Date, error) {
	s := strings.TrimSpace(text)

	if m := isoRe.FindStringSubmatch(s); m != nil {
		return build(num(m[1]), num(m[2]), num(m[3]), false, s)
	}
	if m := dayNameRe.FindStringSubmatch(s); m != nil {
		if mon, ok := months[strings.ToLower(m[2])]; ok {
			return build(num(m[3]), mon, num(m[1]), false, s)
		}
	}
	if m := nameDayRe.FindStringSubmatch(s); m != nil {
		if mon, ok := months[strings.ToLower(m[1])]; ok {
			return build(num(m[3]), mon, num(m[2]), false, s)
		}
	}
	if m := numericRe.FindStringSubmatch(s); m != nil {
		a, b, y := num(m[1]), num(m[2]), num(m[3])
		if len(m[3]) == 2 {
			y += 2000
		}
		day, month := a, b
		if order == MDY {
			day, month = b, a
		}
		// Which readings are valid at all?
		dmyOK := validDate(y, b, a)
		mdyOK := validDate(y, a, b)
		switch {
		case dmyOK && mdyOK && a != b:
			return build(y, month, day, true, s)
		case dmyOK && mdyOK: // a == b: the readings are the same date
			return build(y, month, day, false, s)
		case dmyOK:
			return build(y, b, a, false, s)
		case mdyOK:
			return build(y, a, b, false, s)
		}
	}
	return Date{}, fmt.Errorf("not a date: %q", text)
}

func num(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func validDate(y, m, d int) bool {
	if m < 1 || m > 12 || d < 1 {
		return false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return t.Year() == y && int(t.Month()) == m && t.Day() == d
}

func build(y, m, d int, ambiguous bool, orig string) (Date, error) {
	if !validDate(y, m, d) {
		return Date{}, fmt.Errorf("not a date: %q", orig)
	}
	return Date{ISO: fmt.Sprintf("%04d-%02d-%02d", y, m, d), Ambiguous: ambiguous}, nil
}
