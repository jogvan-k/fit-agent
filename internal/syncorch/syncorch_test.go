package syncorch

import (
	"context"
	"encoding/json"
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

// TestSyncPullCreatesIcuMirror covers the canonical pull path: an icu
// event with no matching local markdown becomes a read-only `.icu.md`
// file. A second sync against the same server reports zero new
// writes (idempotent), and removing the event from icu deletes the
// mirror file.
func TestSyncPullCreatesIcuMirror(t *testing.T) {
	tmp := t.TempDir()
	layout := workspace.New(tmp)
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Server returns one event in range. The empty list is used after
	// `removeEvent` is set true.
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
	r := daterange.Range{Oldest: "2026-05-01", Newest: "2026-05-31"}

	// First run: creates the mirror.
	res, err := Sync(context.Background(), ctx, r)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Pull.Written != 1 || res.Pull.Local != 0 || res.Pull.Removed != 0 {
		t.Errorf("first sync stats unexpected: %+v", res.Pull)
	}
	mirror := layout.PulledWorkoutDayPath(time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC), "4711")
	body, err := os.ReadFile(mirror)
	if err != nil {
		t.Fatalf("expected mirror at %s: %v", mirror, err)
	}
	if !strings.Contains(string(body), "kind: pulled-workout-day") {
		t.Errorf("mirror missing kind: %s", body)
	}
	if !strings.Contains(string(body), "icu_event_id: 4711") {
		t.Errorf("mirror missing event id: %s", body)
	}
	// Cache should be refreshed.
	cachePath := layout.CacheEventPath("4711")
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected cache file %s: %v", cachePath, err)
	}

	// Second run: idempotent, no changes.
	res, err = Sync(context.Background(), ctx, r)
	if err != nil {
		t.Fatalf("Sync 2: %v", err)
	}
	if res.Pull.Written != 0 || res.Pull.Unchanged != 1 {
		t.Errorf("idempotent sync stats unexpected: %+v", res.Pull)
	}

	// Third run: event removed on icu side, mirror should be pruned.
	removeEvent.Store(true)
	res, err = Sync(context.Background(), ctx, r)
	if err != nil {
		t.Fatalf("Sync 3: %v", err)
	}
	if res.Pull.Removed != 1 {
		t.Errorf("expected 1 removed, got %+v", res.Pull)
	}
	if _, err := os.Stat(mirror); !os.IsNotExist(err) {
		t.Errorf("expected mirror to be removed: %v", err)
	}
}

// TestSyncSkipsLocallyAuthored confirms an icu event whose id matches
// a stamped local file is treated as local (no .icu.md is written).
func TestSyncSkipsLocallyAuthored(t *testing.T) {
	tmp := t.TempDir()
	layout := workspace.New(tmp)
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// Local file with id=4711 stamped.
	localPath := layout.PlannedWorkoutDayPath(time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC))
	if err := os.WriteFile(localPath, []byte(`---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-04
workouts:
  - name: "Z2 Endurance"
    type: Ride
    icu_event_id: 4711
---

## Z2 Endurance

`+"```fit-workout\n- 60m Z2\n```\n"), 0o644); err != nil {
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
	res, err := Sync(context.Background(), Context{
		Client: client, AthleteID: "0", Layout: layout, Location: time.UTC,
		Logger: func(string, ...any) {},
	}, daterange.Range{Oldest: "2026-05-01", Newest: "2026-05-31"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Pull.Local != 1 || res.Pull.Written != 0 {
		t.Errorf("expected local=1 written=0, got %+v", res.Pull)
	}
	// No .icu.md should have been created.
	matches, _ := filepath.Glob(filepath.Join(layout.PlannedWorkoutsDir(), "*.icu.md"))
	if len(matches) != 0 {
		t.Errorf("unexpected .icu.md files: %v", matches)
	}
}

func TestParsePulledFilename(t *testing.T) {
	cases := []struct {
		in       string
		date, id string
		ok       bool
	}{
		{"2026-05-04.4711.icu.md", "2026-05-04", "4711", true},
		{"2026-05-04.md", "", "", false},
		{"2026-05-04.something.icu.md", "2026-05-04", "something", true},
		{"random.txt", "", "", false},
		{"2026-05-04.icu.md", "2026-05-04", "", false}, // missing id segment between date and suffix
	}
	for _, tc := range cases {
		gd, gi, ok := ParsePulledFilename(tc.in)
		if gd != tc.date || gi != tc.id || ok != tc.ok {
			t.Errorf("ParsePulledFilename(%q) = (%q,%q,%v); want (%q,%q,%v)", tc.in, gd, gi, ok, tc.date, tc.id, tc.ok)
		}
	}
}

// silenceLogger keeps test output tidy when verbose runs are not needed.
var _ = json.RawMessage(nil) // keep encoding/json import used
