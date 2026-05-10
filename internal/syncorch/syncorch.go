// Package syncorch implements the two-way `fit-agent sync-workouts`
// flow. It pushes agent-authored planned-workouts/*.md to intervals.icu
// (via [pushorch]), then pulls every WORKOUT-category event in range
// and refreshes the workspace to reflect the live icu state.
//
// Push runs first so that workouts the agent just authored are
// returned by the subsequent pull and stamped with their server-
// assigned id in the locally-authored file (handled by pushorch.Apply).
//
// The pull step:
//
//  1. Lists events from icu over the requested range.
//  2. Writes each event to `.cache/events/<id>.json`.
//  3. Removes any `.cache/events/<id>.json` whose icu event has been
//     deleted on the server side and whose start date falls in range.
//  4. Delegates to [renderorch.Planned] to rewrite the machine-owned
//     icu block inside each `planned-workouts/YYYY-MM-DD.md`,
//     preserving every byte the agent owns outside the sentinels.
package syncorch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/pushorch"
	"github.com/jogvan-k/fit-agent/internal/renderorch"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// Context bundles the dependencies needed by Sync.
type Context struct {
	Client    *icu.Client
	AthleteID string
	Layout    workspace.Layout
	Location  *time.Location
	// Now stamps generated_at into the rewritten icu block. Defaults
	// to time.Now() when zero.
	Now time.Time
	// DryRun reports actions without writing to disk or calling icu's
	// mutation endpoints. Read-only icu list calls still happen so the
	// dry-run output reflects what would change.
	DryRun bool
	// Prune is forwarded to the push step: when true, cached events
	// not present in markdown are DELETEd from icu.
	Prune  bool
	Logger func(format string, args ...any)
}

func (c Context) logf(format string, args ...any) {
	if c.Logger != nil {
		c.Logger(format, args...)
	}
}

func (c Context) now() time.Time {
	if c.Now.IsZero() {
		return time.Now()
	}
	return c.Now
}

// Result summarises one Sync run.
type Result struct {
	Push pushorch.Stats
	Pull PullStats
}

// String renders a one-line summary suitable for the CLI.
func (r Result) String() string {
	return fmt.Sprintf("push[%s] pull[%s]", r.Push.String(), r.Pull.String())
}

// PullStats counts what the pull step did.
type PullStats struct {
	// Events is the number of icu events returned by ListEvents.
	Events int
	// CacheRemoved is the number of stale .cache/events/<id>.json
	// files deleted because their icu event no longer exists.
	CacheRemoved int
	// Render reports what the renderorch.Planned delegate did to the
	// planned-workouts/*.md files.
	Render renderorch.Stats
	Errors int
}

// String formats PullStats for a one-line summary.
func (s PullStats) String() string {
	return fmt.Sprintf("events=%d cache_removed=%d render[%s] errors=%d",
		s.Events, s.CacheRemoved, s.Render.String(), s.Errors)
}

// Sync runs the full push-then-pull flow over the supplied range.
func Sync(ctx context.Context, c Context, r daterange.Range) (Result, error) {
	var res Result

	// 1. Push agent-authored markdown to icu.
	pctx := pushorch.Context{
		Client:    c.Client,
		AthleteID: c.AthleteID,
		Layout:    c.Layout,
		Location:  c.Location,
		DryRun:    c.DryRun,
		Prune:     c.Prune,
		Logger:    c.Logger,
	}
	actions, err := pushorch.Plan(ctx, pctx, r)
	if err != nil {
		return res, fmt.Errorf("plan push: %w", err)
	}
	if err := pushorch.Apply(ctx, pctx, actions); err != nil {
		return res, fmt.Errorf("apply push: %w", err)
	}
	res.Push = pushorch.Summarise(actions)

	// 2. Pull from icu and reconcile the workspace.
	pullStats, err := pull(ctx, c, r)
	res.Pull = pullStats
	if err != nil {
		return res, fmt.Errorf("pull: %w", err)
	}
	return res, nil
}

// pull fetches events from icu, refreshes the events cache, prunes
// stale cache entries, and delegates rendering to renderorch.Planned.
func pull(ctx context.Context, c Context, r daterange.Range) (PullStats, error) {
	var stats PullStats
	events, err := c.Client.ListEvents(ctx, c.AthleteID, r.Oldest, r.Newest, icu.EventCategoryWorkout)
	if err != nil {
		return stats, fmt.Errorf("list events: %w", err)
	}
	stats.Events = len(events)

	// Refresh .cache/events/<id>.json for every returned event.
	live := map[int64]bool{}
	for _, ev := range events {
		live[ev.ID] = true
		if err := writeCacheEvent(c.Layout, ev, c.DryRun); err != nil {
			c.logf("event id=%d: cache write failed: %v", ev.ID, err)
			stats.Errors++
		}
	}

	// Remove stale cache entries: any .cache/events/<id>.json whose
	// embedded start_date_local falls in range but whose id is not in
	// the live set.
	removed, err := pruneStaleCache(c.Layout, c.Location, r, live, c.DryRun, c.logf)
	if err != nil {
		return stats, err
	}
	stats.CacheRemoved = removed

	// Delegate to renderorch.Planned: it reads the freshly-updated
	// cache and rewrites the machine block inside each
	// planned-workouts/<date>.md.
	rctx := renderorch.Context{
		Layout:   c.Layout,
		Location: c.Location,
		Now:      c.now(),
		DryRun:   c.DryRun,
		Logger:   c.Logger,
	}
	rstats, err := renderorch.Planned(ctx, rctx, r)
	stats.Render = rstats
	if err != nil {
		return stats, fmt.Errorf("render planned: %w", err)
	}
	return stats, nil
}

func writeCacheEvent(l workspace.Layout, ev icu.Event, dryRun bool) error {
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(l.CacheEventsDir(), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	path := l.CacheEventPath(fmt.Sprintf("%d", ev.ID))
	return workspace.AtomicWrite(path, body, 0)
}

// pruneStaleCache deletes .cache/events/<id>.json files whose icu
// event is no longer returned by ListEvents and whose start_date_local
// falls within r. Files outside the range, or whose JSON cannot be
// decoded, are left untouched.
func pruneStaleCache(l workspace.Layout, loc *time.Location, r daterange.Range, live map[int64]bool, dryRun bool, logf func(string, ...any)) (int, error) {
	dir := l.CacheEventsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read cache events dir: %w", err)
	}
	var removed int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var ev icu.Event
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}
		if live[ev.ID] {
			continue
		}
		date, ok := parseLocalDate(ev.StartDateLocal, loc)
		if !ok {
			continue
		}
		if date.Before(r.OldestT) || date.After(r.NewestT) {
			continue
		}
		if dryRun {
			logf("[dry-run] remove stale cache %s", e.Name())
			removed++
			continue
		}
		if err := os.Remove(path); err != nil {
			logf("remove %s: %v", path, err)
			continue
		}
		removed++
		logf("removed stale cache %s", e.Name())
	}
	return removed, nil
}

// parseLocalDate parses an icu-style start_date_local like
// "2026-05-04T07:00:00" into a date in the supplied location.
func parseLocalDate(s string, loc *time.Location) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	s = strings.SplitN(s, "Z", 2)[0]
	if len(s) > 10 {
		if i := strings.IndexAny(s[10:], "+-"); i > 0 {
			s = s[:10+i]
		}
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", s, loc)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02", s[:min(10, len(s))], loc)
		if err != nil {
			return time.Time{}, false
		}
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc), true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
