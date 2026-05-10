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

func TestPlannedCreatesDayFileWithSentinelBlock(t *testing.T) {
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
	dayPath := c.Layout.PlannedWorkoutDayPath(time.Date(2026, 5, 12, 0, 0, 0, 0, c.Location))
	body, err := os.ReadFile(dayPath)
	if err != nil {
		t.Fatalf("expected day file at %s: %v", dayPath, err)
	}
	// File created with skeleton frontmatter and sentinel block.
	if !bytes.Contains(body, []byte("kind: planned-workout-day")) {
		t.Errorf("missing planned-workout-day frontmatter:\n%s", body)
	}
	if !bytes.Contains(body, []byte("workouts: []")) {
		t.Errorf("missing empty workouts list (skeleton):\n%s", body)
	}
	if !bytes.Contains(body, []byte("<!-- fit-agent:icu:begin -->")) ||
		!bytes.Contains(body, []byte("<!-- fit-agent:icu:end -->")) {
		t.Errorf("missing icu block sentinels:\n%s", body)
	}
	if !bytes.Contains(body, []byte("icu_event_id: 777")) {
		t.Errorf("icu block missing event id:\n%s", body)
	}

	// Second run is idempotent (generated_at line is ignored by the
	// writeRendered hash compare).
	stats2, _ := Planned(context.Background(), c, r)
	if stats2.Added != 0 || stats2.Unchanged != 1 {
		t.Errorf("second run = %+v", stats2)
	}
}

func TestPlannedPreservesAgentContent(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheEventPath("888"), map[string]any{
		"id":               888,
		"category":         "WORKOUT",
		"start_date_local": "2026-05-13T07:00:00",
		"name":             "Tempo",
		"type":             "Run",
		"description":      "- 30m Z3\n",
	})
	// Agent-authored file with frontmatter, prose, and a fit-workout
	// fence — but no sentinel block yet. Planned must append the
	// block and leave everything above it untouched.
	agentSrc := []byte(`---
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
	dayPath := c.Layout.PlannedWorkoutDayPath(time.Date(2026, 5, 13, 0, 0, 0, 0, c.Location))
	if err := workspace.AtomicWrite(dayPath, agentSrc, 0); err != nil {
		t.Fatal(err)
	}
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-31"}, c.Location, c.Now)
	stats, err := Planned(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Updated != 1 {
		t.Errorf("stats = %+v, want one Updated", stats)
	}
	got, _ := os.ReadFile(dayPath)
	// Agent-authored prefix is preserved byte-for-byte.
	if !bytes.HasPrefix(got, agentSrc[:bytes.Index(agentSrc, []byte("```\n"))+4]) {
		t.Errorf("agent content not preserved:\n%s", got)
	}
	// Sentinel block was appended and carries the event id.
	if !bytes.Contains(got, []byte("<!-- fit-agent:icu:begin -->")) {
		t.Errorf("missing begin sentinel:\n%s", got)
	}
	if !bytes.Contains(got, []byte("icu_event_id: 888")) {
		t.Errorf("missing event id in block:\n%s", got)
	}

	// Re-running with a changed cache event re-splices in place.
	writeJSON(t, c.Layout.CacheEventPath("888"), map[string]any{
		"id":               888,
		"category":         "WORKOUT",
		"start_date_local": "2026-05-13T07:00:00",
		"name":             "Tempo (updated)",
		"type":             "Run",
		"description":      "- 35m Z3\n",
	})
	if _, err := Planned(context.Background(), c, r); err != nil {
		t.Fatal(err)
	}
	got2, _ := os.ReadFile(dayPath)
	if !bytes.Contains(got2, []byte("Tempo (updated)")) {
		t.Errorf("updated event not reflected in block:\n%s", got2)
	}
	// Agent prose is still there.
	if !bytes.Contains(got2, []byte("Threshold work.")) {
		t.Errorf("agent prose lost:\n%s", got2)
	}
	// Only one block (sentinel pair) in the file.
	if got, want := bytes.Count(got2, []byte("<!-- fit-agent:icu:begin -->")), 1; got != want {
		t.Errorf("found %d begin sentinels, want %d", got, want)
	}
}

func TestPlannedMultipleEventsSameDay(t *testing.T) {
	c := newCtx(t)
	writeJSON(t, c.Layout.CacheEventPath("100"), map[string]any{
		"id": 100, "category": "WORKOUT",
		"start_date_local": "2026-05-14T07:00:00", "name": "Morning",
	})
	writeJSON(t, c.Layout.CacheEventPath("101"), map[string]any{
		"id": 101, "category": "WORKOUT",
		"start_date_local": "2026-05-14T18:00:00", "name": "Evening",
	})
	r, _ := daterange.Parse(daterange.Inputs{From: "2026-05-01", To: "2026-05-31"}, c.Location, c.Now)
	if _, err := Planned(context.Background(), c, r); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(c.Layout.PlannedWorkoutDayPath(time.Date(2026, 5, 14, 0, 0, 0, 0, c.Location)))
	if !bytes.Contains(body, []byte("icu_event_id: 100")) ||
		!bytes.Contains(body, []byte("icu_event_id: 101")) {
		t.Errorf("expected both events in the block:\n%s", body)
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
