package daterange

import (
	"strings"
	"testing"
	"time"
)

var madrid = mustLoad("Europe/Madrid")

func mustLoad(s string) *time.Location {
	l, err := time.LoadLocation(s)
	if err != nil {
		panic(err)
	}
	return l
}

func date(y int, m time.Month, d int, loc *time.Location) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func TestParseDefault(t *testing.T) {
	now := date(2026, 5, 15, madrid).Add(10 * time.Hour)
	got, err := Parse(Inputs{}, madrid, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Newest != "2026-05-15" {
		t.Errorf("newest = %s", got.Newest)
	}
	// 30 days inclusive: 2026-04-16 .. 2026-05-15
	if got.Oldest != "2026-04-16" {
		t.Errorf("oldest = %s", got.Oldest)
	}
}

func TestParseSince(t *testing.T) {
	now := date(2026, 5, 15, madrid)
	cases := []struct {
		in            string
		wantOldest    string
		wantErrSubstr string
	}{
		{"7d", "2026-05-09", ""},
		{"1w", "2026-05-09", ""},
		{"2W", "2026-05-02", ""},
		{"3m", "2026-02-15", ""},
		{"30", "", "want N(d|w|m)"},
		{"0d", "", "positive integer"},
		{"-7d", "", "want N(d|w|m)"},
	}
	for _, tc := range cases {
		got, err := Parse(Inputs{Since: tc.in}, madrid, now)
		if tc.wantErrSubstr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Errorf("Parse(--since %s) err = %v; want %q", tc.in, err, tc.wantErrSubstr)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(--since %s): %v", tc.in, err)
			continue
		}
		if got.Oldest != tc.wantOldest {
			t.Errorf("Parse(--since %s) oldest = %s; want %s", tc.in, got.Oldest, tc.wantOldest)
		}
	}
}

func TestParseFromTo(t *testing.T) {
	now := date(2026, 5, 15, madrid)
	got, err := Parse(Inputs{From: "2026-04-01", To: "2026-04-30"}, madrid, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Oldest != "2026-04-01" || got.Newest != "2026-04-30" {
		t.Errorf("got %+v", got)
	}
}

func TestParseFromOnly(t *testing.T) {
	now := date(2026, 5, 15, madrid)
	got, err := Parse(Inputs{From: "2026-05-01"}, madrid, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Newest != "2026-05-15" {
		t.Errorf("newest = %s", got.Newest)
	}
}

func TestParseConflicts(t *testing.T) {
	now := date(2026, 5, 15, madrid)
	cases := []Inputs{
		{Since: "7d", From: "2026-04-01"},
		{To: "2026-04-30"},                     // To without From
		{From: "2026-04-30", To: "2026-04-01"}, // From > To
	}
	for i, in := range cases {
		if _, err := Parse(in, madrid, now); err == nil {
			t.Errorf("case %d: expected error for %+v", i, in)
		}
	}
}

func TestMonthsCovered(t *testing.T) {
	now := date(2026, 5, 15, madrid)
	r, _ := Parse(Inputs{From: "2026-03-15", To: "2026-05-10"}, madrid, now)
	months := r.MonthsCovered()
	if len(months) != 3 {
		t.Fatalf("len = %d, months = %v", len(months), months)
	}
	if months[0].Format("2006-01") != "2026-03" || months[2].Format("2006-01") != "2026-05" {
		t.Errorf("months = %v", months)
	}
}

func TestDays(t *testing.T) {
	now := date(2026, 5, 15, madrid)
	r, _ := Parse(Inputs{From: "2026-05-13", To: "2026-05-15"}, madrid, now)
	days := r.Days()
	if len(days) != 3 {
		t.Errorf("len(days) = %d", len(days))
	}
}
