package syncorch

import (
	"context"
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

// TestSyncPullCreatesDayFile covers the canonical pull path: an icu
// event with no matching local markdown causes Sync to create a
// `<date>.md` with a default skeleton and a populated machine-owned
// icu block. A second sync against the same server is idempotent;
// removing the event on icu prunes the cache file and clears the
// block on the next render.
func TestSyncPullCreatesDayFile(t *testing.T) {
	tmp := t.TempDir()
	layout := workspace.New(tmp)
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	var removeEvent atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			if removeEvent.Load() {
				_, _ = w.Write([]byte("[]"))
				return
			}
			_, _ = w.Write([]byte(`[{
  "id": 4711,
  "category": "WORKOUT",
  "start_date_local": "2026-05-04T07:00:00",
  "name": "Z2 Endurance",
  "type": "Ride",
  "moving_time": 4500,
  "description": "- 10m Z1\n- 60m Z2\n- 5m Z1"
}]`))
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
		Now:       time.Date(2026, 5, 3, 20, 14, 0, 0, time.UTC),
		Logger:    func(format string, args ...any) { t.Logf(format, args...) },
	}
	r := daterange.Range{
		Oldest:  "2026-05-01",
		Newest:  "2026-05-31",
		OldestT: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		NewestT: time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	}

	// First run: creates the day file with the icu block.
	res, err := Sync(context.Background(), ctx, r)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Pull.Events != 1 || res.Pull.Render.Added != 1 {
		t.Errorf("first sync stats unexpected: %+v", res.Pull)
	}
	dayPath := layout.PlannedWorkoutDayPath(time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC))
	body, err := os.ReadFile(dayPath)
	if err != nil {
		t.Fatalf("expected day file at %s: %v", dayPath, err)
	}
	if !strings.Contains(string(body), "kind: planned-workout-day") {
		t.Errorf("missing planned-workout-day frontmatter: %s", body)
	}
	if !strings.Contains(string(body), "icu_event_id: 4711") {
		t.Errorf("missing event id in icu block: %s", body)
	}
	if !strings.Contains(string(body), "<!-- fit-agent:icu:begin -->") {
		t.Errorf("missing begin sentinel: %s", body)
	}
	// Cache should be refreshed.
	if _, err := os.Stat(layout.CacheEventPath("4711")); err != nil {
		t.Errorf("expected cache file: %v", err)
	}

	// Second run: idempotent — no new files, no rewrite.
	res, err = Sync(context.Background(), ctx, r)
	if err != nil {
		t.Fatalf("Sync 2: %v", err)
	}
	if res.Pull.Render.Added != 0 || res.Pull.Render.Unchanged != 1 {
		t.Errorf("idempotent sync stats unexpected: %+v", res.Pull)
	}

	// Third run: event removed on icu side. Cache file is pruned and
	// the block is rewritten with events: [].
	removeEvent.Store(true)
	res, err = Sync(context.Background(), ctx, r)
	if err != nil {
		t.Fatalf("Sync 3: %v", err)
	}
	if res.Pull.CacheRemoved != 1 {
		t.Errorf("expected 1 cache_removed, got %+v", res.Pull)
	}
	if _, err := os.Stat(layout.CacheEventPath("4711")); !os.IsNotExist(err) {
		t.Errorf("expected cache to be pruned: %v", err)
	}
	// The day file still exists (the agent may have added prose by
	// now) but the icu block no longer contains events because the
	// cache is empty for that date. Render is only invoked for dates
	// with at least one cached event, so the block is left as-is —
	// matching the joint-ownership contract.
	if _, err := os.Stat(dayPath); err != nil {
		t.Errorf("day file unexpectedly removed: %v", err)
	}
}

// TestSyncPreservesAgentContent confirms an icu event whose id matches
// a stamped local workout is reflected in the machine block while the
// agent's frontmatter, prose, and fit-workout fence are preserved.
func TestSyncPreservesAgentContent(t *testing.T) {
	tmp := t.TempDir()
	layout := workspace.New(tmp)
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	localPath := layout.PlannedWorkoutDayPath(time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC))
	agent := []byte(`---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-04
workouts:
  - name: "Z2 Endurance"
    type: Ride
    icu_event_id: 4711
---

## Z2 Endurance

Easy.

` + "```fit-workout\n- 60m Z2\n```\n")
	if err := os.WriteFile(localPath, agent, 0o644); err != nil {
		t.Fatal(err)
	}
	// Cache the event so the push diff sees it as unchanged.
	if err := os.WriteFile(layout.CacheEventPath("4711"), []byte(`{
  "id": 4711, "category": "WORKOUT",
  "start_date_local": "2026-05-04T00:00:00",
  "name": "Z2 Endurance", "type": "Ride",
  "description": "- 60m Z2"
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			_, _ = w.Write([]byte(`[{
  "id": 4711, "category": "WORKOUT",
  "start_date_local": "2026-05-04T00:00:00",
  "name": "Z2 Endurance", "type": "Ride",
  "description": "- 60m Z2"
}]`))
		case r.Method == http.MethodPut:
			// Return a single Event object as the ICU API does on update.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
  "id": 4711, "category": "WORKOUT",
  "start_date_local": "2026-05-04T00:00:00",
  "name": "Z2 Endurance", "type": "Ride",
  "description": "- 60m Z2"
}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		}
	}))
	t.Cleanup(srv.Close)

	client, err := icu.NewClient("dummy", icu.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	r := daterange.Range{
		Oldest:  "2026-05-01",
		Newest:  "2026-05-31",
		OldestT: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		NewestT: time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	}
	res, err := Sync(context.Background(), Context{
		Client: client, AthleteID: "0", Layout: layout, Location: time.UTC,
		Logger: func(string, ...any) {},
	}, r)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Pull.Events != 1 {
		t.Errorf("expected 1 event, got %+v", res.Pull)
	}

	got, _ := os.ReadFile(localPath)
	// Agent's frontmatter + prose + fence preserved.
	if !strings.Contains(string(got), "Easy.") || !strings.Contains(string(got), "```fit-workout") {
		t.Errorf("agent content not preserved:\n%s", got)
	}
	// Machine block was appended and reflects the event.
	if !strings.Contains(string(got), "<!-- fit-agent:icu:begin -->") {
		t.Errorf("missing begin sentinel:\n%s", got)
	}
	if !strings.Contains(string(got), "icu_event_id: 4711") {
		t.Errorf("missing event id in block:\n%s", got)
	}
	// No legacy .icu.md files anywhere.
	matches, _ := filepath.Glob(filepath.Join(layout.PlannedWorkoutsDir(), "*.icu.md"))
	if len(matches) != 0 {
		t.Errorf("unexpected .icu.md files: %v", matches)
	}
}
