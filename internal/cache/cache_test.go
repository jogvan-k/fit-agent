package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

func newStubServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/athlete/0"):
			fmt.Fprintln(w, `{"id":"i1","name":"Test","timezone":"UTC"}`)

		case strings.HasSuffix(r.URL.Path, "/athlete/i1/activities"):
			fmt.Fprintln(w, `[
				{"id":"a1","name":"Ride","type":"Ride","start_date_local":"2026-05-10T08:00:00","file_type":"fit"},
				{"id":"a2","name":"Manual","type":"Run","start_date_local":"2026-05-11T08:00:00"}
			]`)

		case strings.HasSuffix(r.URL.Path, "/activity/a1"):
			fmt.Fprintln(w, `{"id":"a1","name":"Ride","type":"Ride","start_date_local":"2026-05-10T08:00:00","file_type":"fit","distance":12345}`)

		case strings.HasSuffix(r.URL.Path, "/activity/a2"):
			fmt.Fprintln(w, `{"id":"a2","name":"Manual","type":"Run","start_date_local":"2026-05-11T08:00:00"}`)

		case strings.HasSuffix(r.URL.Path, "/activity/a1/fit-file"):
			_, _ = w.Write([]byte("FAKE-FIT-BYTES"))

		case strings.HasSuffix(r.URL.Path, "/wellness"):
			fmt.Fprintln(w, `[
				{"id":"2026-05-10","restingHR":52,"sleepSecs":27000},
				{"id":"2026-05-11","restingHR":51,"sleepSecs":28000}
			]`)

		case strings.HasSuffix(r.URL.Path, "/events"):
			fmt.Fprintln(w, `[
				{"id":777,"category":"WORKOUT","start_date_local":"2026-05-12T07:00:00","name":"Threshold"}
			]`)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newCacheCtx(t *testing.T, srvURL string) Context {
	t.Helper()
	c, err := icu.NewClient("test-key", icu.Options{BaseURL: srvURL, HTTPClient: &http.Client{Timeout: 5 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	return Context{
		Client:    c,
		Layout:    workspace.New(t.TempDir()),
		AthleteID: "i1",
		Location:  time.UTC,
	}
}

func mustRange(t *testing.T) daterange.Range {
	t.Helper()
	r, err := daterange.Parse(daterange.Inputs{From: "2026-05-10", To: "2026-05-15"}, time.UTC, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAthlete(t *testing.T) {
	srv := newStubServer(t)
	c := newCacheCtx(t, srv.URL)
	out, err := Athlete(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if out != OutcomeAdded {
		t.Errorf("first run outcome = %v", out)
	}
	// Second run with no changes → unchanged.
	out, err = Athlete(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if out != OutcomeUnchanged {
		t.Errorf("second run outcome = %v, want unchanged", out)
	}
	body, err := os.ReadFile(c.Layout.CacheAthletePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"name": "Test"`)) {
		t.Errorf("athlete json missing pretty content: %s", body)
	}
}

func TestActivities(t *testing.T) {
	srv := newStubServer(t)
	c := newCacheCtx(t, srv.URL)
	stats, err := Activities(context.Background(), c, mustRange(t))
	if err != nil {
		t.Fatal(err)
	}
	// 2 JSONs added + 1 FIT added = 3 added.
	if stats.Added != 3 || stats.Errors != 0 {
		t.Errorf("first run stats = %+v", stats)
	}
	if _, err := os.Stat(c.Layout.CacheActivityJSONPath("a1")); err != nil {
		t.Errorf("missing a1 json: %v", err)
	}
	if _, err := os.Stat(c.Layout.CacheActivityJSONPath("a2")); err != nil {
		t.Errorf("missing a2 json: %v", err)
	}
	if _, err := os.Stat(c.Layout.CacheActivityFITPath("a1")); err != nil {
		t.Errorf("missing a1 fit: %v", err)
	}
	if _, err := os.Stat(c.Layout.CacheActivityFITPath("a2")); err == nil {
		t.Errorf("a2 should have no fit (no file_type)")
	}
	// Second run: JSONs unchanged, FIT skipped.
	stats, _ = Activities(context.Background(), c, mustRange(t))
	if stats.Unchanged != 2 || stats.Skipped != 1 {
		t.Errorf("second run stats = %+v", stats)
	}
	// --force-refit → FIT updated (or unchanged if bytes match).
	c.ForceRefit = true
	stats, _ = Activities(context.Background(), c, mustRange(t))
	if stats.Unchanged < 2 {
		t.Errorf("force-refit: want >=2 unchanged json, got %+v", stats)
	}
}

func TestWellnessMerge(t *testing.T) {
	srv := newStubServer(t)
	c := newCacheCtx(t, srv.URL)
	r := mustRange(t)
	// Pre-populate with a day outside the fetch range.
	monthPath := c.Layout.CacheWellnessMonthPath(r.OldestT)
	preload := []map[string]any{
		{"id": "2026-05-01", "restingHR": 49},
	}
	body, _ := json.MarshalIndent(preload, "", "  ")
	if err := workspace.AtomicWrite(monthPath, append(body, '\n'), 0); err != nil {
		t.Fatal(err)
	}
	stats, err := Wellness(context.Background(), c, r)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Errors != 0 {
		t.Errorf("errors = %+v", stats)
	}
	// After merge, the month file should contain all 3 days.
	merged, _ := os.ReadFile(monthPath)
	for _, want := range []string{`"2026-05-01"`, `"2026-05-10"`, `"2026-05-11"`} {
		if !bytes.Contains(merged, []byte(want)) {
			t.Errorf("missing %s in merged wellness:\n%s", want, merged)
		}
	}
}

func TestEvents(t *testing.T) {
	srv := newStubServer(t)
	c := newCacheCtx(t, srv.URL)
	stats, err := Events(context.Background(), c, mustRange(t))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 {
		t.Errorf("stats = %+v", stats)
	}
	if _, err := os.Stat(c.Layout.CacheEventPath("777")); err != nil {
		t.Errorf("missing event 777: %v", err)
	}
}

func TestDryRun(t *testing.T) {
	srv := newStubServer(t)
	c := newCacheCtx(t, srv.URL)
	c.DryRun = true
	stats, err := Activities(context.Background(), c, mustRange(t))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 3 {
		t.Errorf("dry run stats = %+v", stats)
	}
	// No files actually written.
	matches, _ := filepath.Glob(filepath.Join(c.Layout.CacheActivitiesDir(), "*"))
	if len(matches) != 0 {
		t.Errorf("dry run wrote files: %v", matches)
	}
}

func TestSingleActivity(t *testing.T) {
	srv := newStubServer(t)
	c := newCacheCtx(t, srv.URL)
	stats, err := SingleActivity(context.Background(), c, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 2 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestAll(t *testing.T) {
	srv := newStubServer(t)
	c := newCacheCtx(t, srv.URL)
	stats, err := All(context.Background(), c, mustRange(t))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Errors != 0 {
		t.Errorf("errors = %+v", stats)
	}
	// Idempotency: second run should report zero added/updated.
	stats2, _ := All(context.Background(), c, mustRange(t))
	if stats2.Added != 0 || stats2.Updated != 0 {
		t.Errorf("second run not idempotent: %+v", stats2)
	}
}
