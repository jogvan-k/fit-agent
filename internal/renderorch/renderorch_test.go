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

func TestPlannedWritesPulledMirrorForUnownedEvent(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheEventPath("777"), map[string]any{
		"id":               777,
		"category":         "WORKOUT",
		"start_date_local": "2026-05-12T07:00:00",
		"name":             "Threshold",
		"type":             "Ride",
		"description":      "- 5m Z2\n- 20m Z4\n- 5m Z2\n",
	})
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-31"}, c.Location, c.Now)
	stats, err := Planned(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 || stats.Errors != 0 {
		t.Errorf("first run = %+v", stats)
	}
	pulledPath := c.Layout.PulledWorkoutDayPath(time.Date(2026, 5, 12, 0, 0, 0, 0, c.Location), "777")
	body, err := os.ReadFile(pulledPath)
	if err != nil {
		t.Fatalf("expected pulled mirror at %s: %v", pulledPath, err)
	}
	if !bytes.Contains(body, []byte("kind: pulled-workout-day")) {
		t.Errorf("mirror body missing pulled-workout-day frontmatter:\n%s", body)
	}
	if !bytes.Contains(body, []byte("read_only: true")) {
		t.Errorf("mirror body missing read_only flag:\n%s", body)
	}

	// Sanity: no agent-owned file was created in the place of the mirror.
	plainPath := c.Layout.PlannedWorkoutDayPath(time.Date(2026, 5, 12, 0, 0, 0, 0, c.Location))
	if _, err := os.Stat(plainPath); !os.IsNotExist(err) {
		t.Errorf("unexpected agent-owned file at %s", plainPath)
	}

	// Second run is idempotent.
	stats2, _ := Planned(context.Background(), c, r)
	if stats2.Added != 0 || stats2.Unchanged != 1 {
		t.Errorf("second run = %+v", stats2)
	}
}

func TestPlannedSkipsEventOwnedByLocalMarkdown(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheEventPath("888"), map[string]any{
		"id":               888,
		"category":         "WORKOUT",
		"start_date_local": "2026-05-13T07:00:00",
		"name":             "Tempo",
		"type":             "Run",
	})
	// Agent-authored file that claims the event by stamped id.
	local := []byte(`---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-13
workouts:
  - name: Tempo
    type: Run
    moving_time_s: 2400
    icu_event_id: 888
---

## Tempo

Threshold work.

` + "```fit-workout\n- 30m Z3\n```\n")
	plainPath := c.Layout.PlannedWorkoutDayPath(time.Date(2026, 5, 13, 0, 0, 0, 0, c.Location))
	if err := workspace.AtomicWrite(plainPath, local, 0); err != nil {
		t.Fatal(err)
	}
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-31"}, c.Location, c.Now)
	stats, err := Planned(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 0 || stats.Unchanged != 1 {
		t.Errorf("stats = %+v", stats)
	}
	// Local file untouched.
	got, _ := os.ReadFile(plainPath)
	if !bytes.Equal(got, local) {
		t.Errorf("local file modified:\n%s", got)
	}
	// No mirror created.
	pulledPath := c.Layout.PulledWorkoutDayPath(time.Date(2026, 5, 13, 0, 0, 0, 0, c.Location), "888")
	if _, err := os.Stat(pulledPath); !os.IsNotExist(err) {
		t.Errorf("unexpected mirror at %s", pulledPath)
	}
}

func TestPlannedPrunesStaleMirror(t *testing.T) {
	c := newCtx(t)
	// Pre-existing mirror file with no matching cache event.
	stalePath := c.Layout.PulledWorkoutDayPath(time.Date(2026, 5, 14, 0, 0, 0, 0, c.Location), "999")
	if err := workspace.AtomicWrite(stalePath, []byte("stale mirror\n"), 0); err != nil {
		t.Fatal(err)
	}
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-31"}, c.Location, c.Now)
	stats, err := Planned(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Errorf("Removed = %d, want 1; stats=%+v", stats.Removed, stats)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("stale mirror not pruned: %v", err)
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
