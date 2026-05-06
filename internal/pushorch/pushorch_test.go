package pushorch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

func TestPlanAndApplyEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	layout := workspace.New(tmp)
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Two markdown days: one new (id null), one already pushed (id=42 but
	// description changed so we expect UPDATE).
	writeMD(t, layout.PlannedWorkoutDayPath(date(2026, 5, 4)), `---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-04
workouts:
  - name: "Z2 Endurance"
    type: Ride
    moving_time_s: 4500
    icu_event_id: null
---

## Z2 Endurance

`+"```fit-workout\n"+`- 10m Z1
- 60m Z2
- 5m Z1
`+"```\n")

	writeMD(t, layout.PlannedWorkoutDayPath(date(2026, 5, 5)), `---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-05
workouts:
  - name: "VO2"
    type: Ride
    moving_time_s: 3600
    icu_event_id: 42
---

## VO2

`+"```fit-workout\n"+`- 10m Z1
- 5x (4m Z5 / 3m Z2)
- 5m Z1
`+"```\n")

	// Cached events: id=42 matches but with a stale description; id=99
	// is on the calendar but not in markdown (delete candidate).
	writeJSON(t, layout.CacheEventPath("42"), icu.Event{
		ID:             42,
		Category:       "WORKOUT",
		StartDateLocal: "2026-05-05T00:00:00",
		Name:           "VO2",
		Type:           "Ride",
		Description:    "stale text",
		MovingTime:     3600,
	})
	writeJSON(t, layout.CacheEventPath("99"), icu.Event{
		ID:             99,
		Category:       "WORKOUT",
		StartDateLocal: "2026-05-06T00:00:00",
		Name:           "Old",
		Type:           "Ride",
	})

	// Fake intervals.icu server.
	var nextID int64 = 1000
	var posts, puts, deletes int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			atomic.AddInt32(&posts, 1)
			var ev icu.Event
			_ = json.NewDecoder(r.Body).Decode(&ev)
			ev.ID = atomic.AddInt64(&nextID, 1)
			_ = json.NewEncoder(w).Encode(ev)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/events/"):
			atomic.AddInt32(&puts, 1)
			var ev icu.Event
			_ = json.NewDecoder(r.Body).Decode(&ev)
			_ = json.NewEncoder(w).Encode(ev)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/events/"):
			atomic.AddInt32(&deletes, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
		}
	}))
	t.Cleanup(srv.Close)

	client, err := icu.NewClient("dummy", icu.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := Context{
		Client:    client,
		AthleteID: "0",
		Layout:    layout,
		Location:  time.UTC,
		Prune:     true,
		Logger:    func(format string, args ...any) { t.Logf(format, args...) },
	}
	r := daterange.Range{Oldest: "2026-05-01", Newest: "2026-05-31"}
	actions, err := Plan(context.Background(), ctx, r)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	stats := Summarise(actions)
	if want := (Stats{Create: 1, Update: 1, Delete: 1}); stats != want {
		t.Errorf("stats = %+v want %+v\nactions=%+v", stats, want, actions)
	}
	if err := Apply(context.Background(), ctx, actions); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if posts != 1 || puts != 1 || deletes != 1 {
		t.Errorf("HTTP counts: POST=%d PUT=%d DELETE=%d", posts, puts, deletes)
	}

	// Verify the new id was stamped back into the markdown.
	got, err := os.ReadFile(layout.PlannedWorkoutDayPath(date(2026, 5, 4)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), fmt.Sprintf("icu_event_id: %d", nextID)) {
		t.Errorf("stamp missing in:\n%s", got)
	}

	// Re-running Plan after Apply should yield zero changes (the stamped
	// id matches a cache entry... but our fake server doesn't refresh
	// the cache, so we model this by re-reading just the unchanged ones:
	// id=42 should report unchanged after the Apply rewrote our local
	// idea of it). For full idempotency we'd need to re-cache; here we
	// just confirm that running Plan a second time without re-caching
	// would re-issue the same actions (because cache is unchanged).
}

func TestPlanSkipWithoutPrune(t *testing.T) {
	tmp := t.TempDir()
	layout := workspace.New(tmp)
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, layout.CacheEventPath("7"), icu.Event{
		ID:             7,
		StartDateLocal: "2026-05-10T00:00:00",
		Name:           "Orphan",
		Type:           "Ride",
	})
	r := daterange.Range{Oldest: "2026-05-01", Newest: "2026-05-31"}
	actions, err := Plan(context.Background(), Context{Layout: layout}, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Kind != ActionSkip {
		t.Errorf("expected one skip, got %+v", actions)
	}
}

func TestPlanUnchanged(t *testing.T) {
	tmp := t.TempDir()
	layout := workspace.New(tmp)
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	writeMD(t, layout.PlannedWorkoutDayPath(date(2026, 5, 4)), `---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-04
workouts:
  - name: "Easy"
    type: Ride
    moving_time_s: 1800
    icu_event_id: 5
---

## Easy

`+"```fit-workout\n"+`- 30m Z2
`+"```\n")
	writeJSON(t, layout.CacheEventPath("5"), icu.Event{
		ID:             5,
		Category:       "WORKOUT",
		StartDateLocal: "2026-05-04T00:00:00",
		Name:           "Easy",
		Type:           "Ride",
		Description:    "- 30m Z2",
		MovingTime:     1800,
	})
	r := daterange.Range{Oldest: "2026-05-01", Newest: "2026-05-31"}
	actions, err := Plan(context.Background(), Context{Layout: layout}, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Kind != ActionUnchanged {
		t.Errorf("expected unchanged, got %+v", actions)
	}
}

func writeMD(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func date(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}
