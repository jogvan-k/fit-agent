// Package daterange parses CLI date-range arguments into [Range]
// values, which are inclusive [oldest, newest] pairs of YYYY-MM-DD
// strings (the format intervals.icu's list endpoints expect).
//
// Supported inputs:
//
//   - --since 30d          → last 30 days through today
//   - --since 12w          → last 12 weeks
//   - --since 6m           → last 6 months (~30d each)
//   - --from 2026-04-01 --to 2026-04-30
//   - --from 2026-04-01    → from that date through today
//   - (no flags)           → DefaultDays back through today
//
// All arithmetic happens in the supplied location; "today" is the
// athlete-local current date. The package intentionally does NOT
// support relative single-day specifiers (yesterday, etc.); use --from
// for that.
package daterange

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultDays is the lookback window used when no flags are passed.
const DefaultDays = 30

// DateLayout is the YYYY-MM-DD format used by intervals.icu list APIs.
const DateLayout = "2006-01-02"

// Range is an inclusive date interval expressed as YYYY-MM-DD strings.
type Range struct {
	// Oldest is the earliest day to include (YYYY-MM-DD).
	Oldest string
	// Newest is the latest day to include (YYYY-MM-DD).
	Newest string
	// OldestT and NewestT are the parsed dates for callers that need
	// to iterate or compute month boundaries; both have hour=0 in loc.
	OldestT time.Time
	NewestT time.Time
}

// Inputs are the raw flag values from the CLI.
type Inputs struct {
	Since string // e.g. "30d", "12w", "6m"
	From  string // "YYYY-MM-DD"
	To    string // "YYYY-MM-DD"
}

// Parse resolves the inputs into a [Range] using the supplied
// location and "now" (use time.Now in production, a fixed value in
// tests). loc must not be nil; pass time.UTC to opt out of TZ logic.
//
// Validation rules:
//
//   - --since may not be combined with --from/--to.
//   - --from > --to is an error.
//   - --since must match (\d+)([dwm]).
func Parse(in Inputs, loc *time.Location, now time.Time) (Range, error) {
	if loc == nil {
		return Range{}, fmt.Errorf("daterange.Parse: loc is required")
	}
	today := now.In(loc)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)

	if in.Since != "" && (in.From != "" || in.To != "") {
		return Range{}, fmt.Errorf("--since cannot be combined with --from/--to")
	}

	switch {
	case in.Since != "":
		days, err := parseSince(in.Since)
		if err != nil {
			return Range{}, err
		}
		return rangeFromDays(today, days, loc), nil

	case in.From != "":
		from, err := parseDate(in.From, loc)
		if err != nil {
			return Range{}, fmt.Errorf("--from: %w", err)
		}
		to := today
		if in.To != "" {
			to, err = parseDate(in.To, loc)
			if err != nil {
				return Range{}, fmt.Errorf("--to: %w", err)
			}
		}
		if from.After(to) {
			return Range{}, fmt.Errorf("--from %s is after --to %s", in.From, to.Format(DateLayout))
		}
		return Range{
			Oldest:  from.Format(DateLayout),
			Newest:  to.Format(DateLayout),
			OldestT: from, NewestT: to,
		}, nil

	case in.To != "":
		return Range{}, fmt.Errorf("--to requires --from")

	default:
		return rangeFromDays(today, DefaultDays, loc), nil
	}
}

// AddFlags wires Inputs into a cobra-style flag set. The fs argument
// is intentionally typed as *pflag.FlagSet via interface to keep this
// package free of cobra/pflag for testability; commands use the
// matching helper in internal/cli.
//
// This stub exists so callers can grep for it; actual flag wiring
// lives in internal/cli/range_flags.go.
//
// (no-op placeholder)
func (Inputs) AddFlags() {}

func parseSince(s string) (int, error) {
	re := regexp.MustCompile(`^(\d+)([dwm])$`)
	m := re.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return 0, fmt.Errorf("--since %q: want N(d|w|m), e.g. 30d, 12w, 6m", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("--since %q: positive integer required", s)
	}
	switch m[2] {
	case "d":
		return n, nil
	case "w":
		return n * 7, nil
	case "m":
		return n * 30, nil
	}
	return 0, fmt.Errorf("--since %q: unknown unit", s)
}

func parseDate(s string, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation(DateLayout, strings.TrimSpace(s), loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return t, nil
}

func rangeFromDays(today time.Time, days int, loc *time.Location) Range {
	from := today.AddDate(0, 0, -(days - 1))
	return Range{
		Oldest:  from.Format(DateLayout),
		Newest:  today.Format(DateLayout),
		OldestT: from, NewestT: today,
	}
}

// MonthsCovered returns the unique calendar months (year+month) that
// the range spans, in ascending order. Each returned time has day=1
// at hour=0 in the same location as r.OldestT.
func (r Range) MonthsCovered() []time.Time {
	if r.OldestT.IsZero() || r.NewestT.IsZero() {
		return nil
	}
	loc := r.OldestT.Location()
	start := time.Date(r.OldestT.Year(), r.OldestT.Month(), 1, 0, 0, 0, 0, loc)
	end := time.Date(r.NewestT.Year(), r.NewestT.Month(), 1, 0, 0, 0, 0, loc)
	out := []time.Time{}
	for !start.After(end) {
		out = append(out, start)
		start = start.AddDate(0, 1, 0)
	}
	return out
}

// Days returns each calendar date in the range in ascending order.
func (r Range) Days() []time.Time {
	if r.OldestT.IsZero() || r.NewestT.IsZero() {
		return nil
	}
	out := []time.Time{}
	for d := r.OldestT; !d.After(r.NewestT); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}
