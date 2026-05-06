package renderorch

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

func newCtx(t *testing.T) Context {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Madrid")
	return Context{
		Layout:   workspace.New(t.TempDir()),
		Location: loc,
		Now:      time.Date(2026, 5, 15, 9, 0, 0, 0, loc),
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.AtomicWrite(path, append(body, '\n'), 0); err != nil {
		t.Fatal(err)
	}
}

func TestActivitiesRendersDayFile(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheActivityJSONPath("a1"), map[string]any{
		"id":               "a1",
		"name":             "Morning ride",
		"type":             "Ride",
		"start_date_local": "2026-05-10T07:30:00",
		"distance":         12345.0,
	})
	writeJSON(t, c.Layout.CacheActivityJSONPath("a2"), map[string]any{
		"id":               "a2",
		"name":             "Out of range",
		"type":             "Run",
		"start_date_local": "2025-01-01T07:30:00",
	})
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-15"}, c.Location, c.Now)
	stats, err := Activities(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 || stats.Errors != 0 {
		t.Errorf("stats = %+v", stats)
	}
	dayPath := c.Layout.ActivityDayPath(time.Date(2026, 5, 10, 0, 0, 0, 0, c.Location))
	body, err := os.ReadFile(dayPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Errorf("empty day file")
	}
	// Idempotent.
	stats2, _ := Activities(context.Background(), c, r)
	if stats2.Unchanged != 1 || stats2.Added != 0 {
		t.Errorf("second run = %+v", stats2)
	}
}

func TestWellnessRendersMonth(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheWellnessMonthPath(c.Now), []map[string]any{
		{"id": "2026-05-10", "restingHR": 50, "sleepSecs": 27000},
		{"id": "2026-05-11", "restingHR": 51, "sleepSecs": 26000},
	})
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-15"}, c.Location, c.Now)
	stats, err := Wellness(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 {
		t.Errorf("stats = %+v", stats)
	}
	body, err := os.ReadFile(c.Layout.WellnessMonthPath(c.Now))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("2026-05-10")) || !bytes.Contains(body, []byte("resting_hr: 50")) {
		t.Errorf("rendered wellness missing data:\n%s", body)
	}
}

func TestPlannedRespectsExistingFile(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheEventPath("777"), map[string]any{
		"id":               777,
		"category":         "WORKOUT",
		"start_date_local": "2026-05-12T07:00:00",
		"name":             "Threshold",
		"type":             "Ride",
	})
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-31"}, c.Location, c.Now)
	stats, err := Planned(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 {
		t.Errorf("first run = %+v", stats)
	}
	// User edits the file; second run must NOT clobber.
	plannedPath := c.Layout.PlannedWorkoutDayPath(time.Date(2026, 5, 12, 0, 0, 0, 0, c.Location))
	want := []byte("EDITED BY AGENT\n")
	if err := workspace.AtomicWrite(plannedPath, want, 0); err != nil {
		t.Fatal(err)
	}
	stats2, _ := Planned(context.Background(), c, r)
	if stats2.Unchanged != 1 || stats2.Added != 0 {
		t.Errorf("second run = %+v", stats2)
	}
	got, _ := os.ReadFile(plannedPath)
	if string(got) != string(want) {
		t.Errorf("planned file overwritten; got:\n%s", got)
	}
}

func TestSingleActivityReturnsBytes(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheActivityJSONPath("a1"), map[string]any{
		"id":               "a1",
		"name":             "Run",
		"type":             "Run",
		"start_date_local": "2026-05-10T07:30:00",
	})
	body, out, err := SingleActivity(context.Background(), c, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if out != OutcomeAdded {
		t.Errorf("outcome = %v", out)
	}
	if len(body) == 0 {
		t.Errorf("empty body")
	}
}

func TestAllRunsEverything(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheActivityJSONPath("a1"), map[string]any{
		"id":               "a1",
		"name":             "Ride",
		"type":             "Ride",
		"start_date_local": "2026-05-10T07:30:00",
	})
	writeJSON(t, c.Layout.CacheWellnessMonthPath(c.Now), []map[string]any{
		{"id": "2026-05-10", "restingHR": 50},
	})
	writeJSON(t, c.Layout.CacheEventPath("777"), map[string]any{
		"id": 777, "category": "WORKOUT",
		"start_date_local": "2026-05-12T07:00:00", "name": "X",
	})
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-31"}, c.Location, c.Now)
	stats, err := All(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 3 || stats.Errors != 0 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestParseLocalDate(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Madrid")
	cases := []struct {
		in   string
		want string
	}{
		{"2026-05-10T07:30:00", "2026-05-10"},
		{"2026-05-10", "2026-05-10"},
		{"2026-05-10T07:30:00Z", "2026-05-10"},
		{"2026-05-10T07:30:00+02:00", "2026-05-10"},
	}
	for _, tc := range cases {
		got, err := parseLocalDate(tc.in, loc)
		if err != nil {
			t.Errorf("parse %q: %v", tc.in, err)
			continue
		}
		if got.Format(daterange.DateLayout) != tc.want {
			t.Errorf("parse %q = %s, want %s", tc.in, got.Format(daterange.DateLayout), tc.want)
		}
	}
}
