package syncorch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

type guardedFixture struct {
	c      Context
	r      daterange.Range
	path   string
	mu     sync.Mutex
	live   []icu.Event
	writes int
}

func newGuardedFixture(t *testing.T) *guardedFixture {
	t.Helper()
	f := &guardedFixture{live: []icu.Event{}}
	l := workspace.New(t.TempDir())
	if err := l.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	f.path = l.PlannedWorkoutDayPath(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	body := "---\nfit-agent:\n  kind: planned-workout-day\n  date: 2026-04-01\nworkouts:\n  - name: Easy\n    type: Ride\n---\n\n## Easy\n\n```fit-workout\n- 60m Z2\n```\n"
	if err := os.WriteFile(f.path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			if err := json.NewEncoder(w).Encode(f.live); err != nil {
				t.Error(err)
			}
		case http.MethodPost, http.MethodPut:
			var ev icu.Event
			if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			ev.ID = 123
			f.live = []icu.Event{ev}
			f.writes++
			if err := json.NewEncoder(w).Encode(ev); err != nil {
				t.Error(err)
			}
		case http.MethodDelete:
			f.live = []icu.Event{}
			f.writes++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := icu.NewClient("dummy", icu.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	f.c = Context{Client: client, AthleteID: "0", Layout: l, Location: time.UTC, DryRun: true}
	f.r = daterange.Range{Oldest: "2026-04-01", Newest: "2026-04-07", OldestT: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), NewestT: time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)}
	return f
}

func (f *guardedFixture) preview(t *testing.T) GuardedResult {
	t.Helper()
	c := f.c
	c.DryRun = true
	p, err := GuardedSync(context.Background(), c, f.r, "0.1.0/default", "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *guardedFixture) apply(t *testing.T, fingerprint string) GuardedResult {
	t.Helper()
	c := f.c
	c.DryRun = false
	p, err := GuardedSync(context.Background(), c, f.r, "0.1.0/default", fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGuardedCreateUpdateDelete(t *testing.T) {
	f := newGuardedFixture(t)
	original, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	p := f.preview(t)
	if len(p.Actions) != 1 || p.Actions[0].Kind != "create" {
		t.Fatalf("unexpected preview: %+v", p)
	}
	if p.Fingerprint != f.preview(t).Fingerprint {
		t.Fatal("preview is not deterministic")
	}
	after, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(after) || f.writes != 0 {
		t.Fatal("preview mutated state")
	}
	if _, err := os.Stat(f.c.Layout.CacheEventPath("123")); !os.IsNotExist(err) {
		t.Fatal("preview wrote cache")
	}
	applied := f.apply(t, p.Fingerprint)
	if applied.Actions[0].EventID != 123 {
		t.Fatal("created event ID missing")
	}
	body, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "icu_event_id: 123") {
		t.Fatal("id not stamped")
	}
	if err := os.WriteFile(f.path, []byte(strings.Replace(string(body), "- 60m Z2", "- 45m Z2", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	p = f.preview(t)
	if p.Actions[0].Kind != "conflict" {
		t.Fatalf("guarded update must fail closed without a remote revision precondition: %+v", p)
	}
	f.c.DryRun = false
	if _, err := GuardedSync(context.Background(), f.c, f.r, "0.1.0/default", p.Fingerprint); !errors.Is(err, ErrConflict) {
		t.Fatalf("guarded update got %v, want conflict", err)
	}
	if err := os.Remove(f.path); err != nil {
		t.Fatal(err)
	}
	f.c.Prune = true
	f.c.DryRun = true
	p = f.preview(t)
	if p.Actions[0].Kind != "conflict" {
		t.Fatalf("guarded delete must fail closed without a remote revision precondition: %+v", p)
	}
	if f.writes != 1 {
		t.Fatalf("guarded protocol made %d writes, want only initial create", f.writes)
	}
}

func TestGuardedStaleRejectsBeforeWrites(t *testing.T) {
	for _, change := range []string{"missing", "file", "remote", "version", "range"} {
		t.Run(change, func(t *testing.T) {
			f := newGuardedFixture(t)
			p := f.preview(t)
			version := "0.1.0/default"
			switch change {
			case "missing":
				p.Fingerprint = ""
			case "file":
				file, err := os.OpenFile(f.path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteString("\nChanged intent\n"); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			case "remote":
				f.mu.Lock()
				f.live = []icu.Event{{ID: 999, Name: "Remote", Category: "WORKOUT", StartDateLocal: "2026-04-02T00:00:00"}}
				f.mu.Unlock()
			case "version":
				version = "0.2.0/default"
			case "range":
				f.r.Newest = "2026-04-08"
			}
			f.c.DryRun = false
			_, err := GuardedSync(context.Background(), f.c, f.r, version, p.Fingerprint)
			if !errors.Is(err, ErrStalePreview) {
				t.Fatalf("got %v", err)
			}
			if f.writes != 0 {
				t.Fatal("stale preview caused mutation")
			}
		})
	}
}

func TestGuardedRemoteConflictSurvivesFetch(t *testing.T) {
	f := newGuardedFixture(t)
	f.apply(t, f.preview(t).Fingerprint)
	f.mu.Lock()
	f.live[0].Description = "athlete-authored remote edit"
	remote := f.live[0]
	f.mu.Unlock()
	// Simulate a fetch updating the ordinary event cache. The last confirmed
	// sync baseline must remain authoritative for conflict detection.
	data, err := json.Marshal(remote)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.AtomicWrite(f.c.Layout.CacheEventPath("123"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	p := f.preview(t)
	if p.Actions[0].Kind != "conflict" {
		t.Fatalf("remote edit was not a conflict: %+v", p)
	}
	f.c.DryRun = false
	_, err = GuardedSync(context.Background(), f.c, f.r, "0.1.0/default", p.Fingerprint)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v", err)
	}
	if f.writes != 1 {
		t.Fatal("conflict was pushed")
	}
}

func TestGuardedRemoteDeletionAndMissingBaseline(t *testing.T) {
	for _, mode := range []string{"deleted", "missing-baseline"} {
		t.Run(mode, func(t *testing.T) {
			f := newGuardedFixture(t)
			f.apply(t, f.preview(t).Fingerprint)
			if mode == "deleted" {
				f.mu.Lock()
				f.live = []icu.Event{}
				f.mu.Unlock()
			} else {
				if err := os.Remove(f.c.Layout.SyncBaselinePath("123")); err != nil {
					t.Fatal(err)
				}
				f.mu.Lock()
				f.live[0].Description = "changed remotely"
				f.mu.Unlock()
			}
			if p := f.preview(t); p.Actions[0].Kind != "conflict" {
				t.Fatalf("expected conflict: %+v", p)
			}
		})
	}
}

func TestGuardedBoundsAndUnsafeFiles(t *testing.T) {
	for _, r := range []daterange.Range{{}, {Oldest: "2026-02-30", Newest: "2026-03-01"}, {Oldest: "2026-04-01", Newest: "2026-03-01"}, {Oldest: "2026-01-01", Newest: "2026-04-01"}} {
		f := newGuardedFixture(t)
		if _, err := GuardedSync(context.Background(), f.c, r, "0.1.0", ""); err == nil {
			t.Fatal("accepted unbounded/invalid range")
		}
	}
	f := newGuardedFixture(t)
	if err := os.Remove(f.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", f.path); err != nil {
		t.Fatal(err)
	}
	if _, err := GuardedSync(context.Background(), f.c, f.r, "0.1.0", ""); err == nil {
		t.Fatal("accepted symlink")
	}
}
