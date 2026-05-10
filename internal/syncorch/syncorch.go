// Package syncorch implements the two-way `fit-agent sync-workouts`
// flow: it pushes agent-authored planned-workouts/*.md to intervals.icu
// (via [pushorch]), then pulls every WORKOUT-category event in range
// from intervals.icu and materialises the ones the agent did NOT
// author locally as read-only `.icu.md` files alongside the agent's
// own files.
//
// Push runs first so that workouts the agent just authored are
// returned by the subsequent pull and stamped with their server-
// assigned id in the locally-authored file (already handled by
// pushorch.Apply).
//
// The pull step is the source of truth for the planned-workouts/
// directory's read-only contents:
//
//   - Each icu event with no matching locally-authored file (matched by
//     icu_event_id stamped in frontmatter, or by (date, name) when no
//     id is stamped) is rendered to a `.icu.md` file.
//   - Any pre-existing `.icu.md` file in the date range that does NOT
//     correspond to an event returned by icu is removed (it represents
//     a workout that has been deleted on the icu side).
//   - The matching `.cache/events/<id>.json` is refreshed to mirror the
//     live response.
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
	"github.com/jogvan-k/fit-agent/internal/plannedio"
	"github.com/jogvan-k/fit-agent/internal/pushorch"
	"github.com/jogvan-k/fit-agent/internal/render"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// Context bundles the dependencies needed by Sync.
type Context struct {
	Client    *icu.Client
	AthleteID string
	Layout    workspace.Layout
	Location  *time.Location
	// Now stamps generated_at into rendered .icu.md files. Defaults
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
	// Written is the number of .icu.md files newly added or rewritten.
	Written int
	// Unchanged is the number of .icu.md files that already matched.
	Unchanged int
	// Removed is the number of stale .icu.md files deleted because
	// the corresponding icu event no longer exists in range.
	Removed int
	// Local is the number of icu events that were skipped because a
	// locally-authored .md file owns them (matched by id or date+name).
	Local  int
	Errors int
}

// String formats PullStats for a one-line summary.
func (s PullStats) String() string {
	return fmt.Sprintf("written=%d unchanged=%d removed=%d local=%d errors=%d",
		s.Written, s.Unchanged, s.Removed, s.Local, s.Errors)
}

// Sync runs the full push-then-pull flow over the supplied range.
//
// The push step is identical to `fit-agent push-workouts`: it diffs
// markdown against the cached event snapshot and applies create /
// update / delete actions. The pull step then re-fetches events from
// icu (so the snapshot reflects anything the push just created) and
// writes / removes `.icu.md` files for events that are not authored
// locally.
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

	// 2. Pull from icu and materialise read-only .icu.md files.
	pullStats, err := pull(ctx, c, r)
	res.Pull = pullStats
	if err != nil {
		return res, fmt.Errorf("pull: %w", err)
	}
	return res, nil
}

// pull fetches events from icu and reconciles them against the
// workspace.
//
// It is intentionally tolerant: a single broken event is logged and
// counted in PullStats.Errors but does not abort the loop.
func pull(ctx context.Context, c Context, r daterange.Range) (PullStats, error) {
	var stats PullStats
	events, err := c.Client.ListEvents(ctx, c.AthleteID, r.Oldest, r.Newest, icu.EventCategoryWorkout)
	if err != nil {
		return stats, fmt.Errorf("list events: %w", err)
	}

	// Build the set of icu_event_ids and (date,name) keys claimed by
	// locally-authored markdown so we know which icu events to skip.
	localIDs, localKeys, err := IndexLocalMarkdown(c.Layout.PlannedWorkoutsDir(), r)
	if err != nil {
		return stats, err
	}

	// Track which event ids we should retain on disk so we can prune
	// stale .icu.md files at the end.
	retainID := map[int64]bool{}

	for _, ev := range events {
		date, ok := parseLocalDate(ev.StartDateLocal, c.Location)
		if !ok {
			c.logf("event id=%d: unparseable start_date_local %q", ev.ID, ev.StartDateLocal)
			stats.Errors++
			continue
		}
		// Locally-authored files own their icu event; don't write a
		// read-only mirror.
		if localIDs[ev.ID] || localKeys[dateNameKey(date, ev.Name)] {
			stats.Local++
			continue
		}
		retainID[ev.ID] = true

		// Refresh the cached raw JSON so subsequent push / render
		// runs see the latest server state.
		if err := writeCacheEvent(c.Layout, ev, c.DryRun); err != nil {
			c.logf("event id=%d: cache write failed: %v", ev.ID, err)
			stats.Errors++
			continue
		}

		body, err := render.PulledWorkoutDayMarkdown(render.PulledWorkout{
			Event:       ev,
			Date:        date,
			GeneratedAt: c.now(),
		})
		if err != nil {
			c.logf("event id=%d: render failed: %v", ev.ID, err)
			stats.Errors++
			continue
		}
		path := c.Layout.PulledWorkoutDayPath(date, fmt.Sprintf("%d", ev.ID))
		written, err := WritePulledFile(path, body, c.DryRun)
		if err != nil {
			c.logf("event id=%d: write failed: %v", ev.ID, err)
			stats.Errors++
			continue
		}
		if written {
			stats.Written++
			c.logf("pulled %s id=%d -> %s", date.Format("2006-01-02"), ev.ID, filepath.Base(path))
		} else {
			stats.Unchanged++
		}
	}

	// Prune stale .icu.md files: any *.icu.md in the planned-workouts
	// directory whose date is in range and whose id is not in retainID
	// represents an event that has been deleted from icu.
	removed, err := PruneStalePulled(c.Layout.PlannedWorkoutsDir(), r, retainID, c.DryRun, c.logf)
	if err != nil {
		return stats, err
	}
	stats.Removed = removed
	return stats, nil
}

// IndexLocalMarkdown returns the set of icu_event_ids claimed by
// agent-authored .md files (excluding .icu.md) and a parallel set of
// (date, name) keys for unstamped workouts.
//
// Exported so renderorch (and other callers) can decide whether a
// cached icu event is already owned by a local file before writing a
// read-only `.icu.md` mirror.
func IndexLocalMarkdown(dir string, r daterange.Range) (map[int64]bool, map[string]bool, error) {
	ids := map[int64]bool{}
	keys := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ids, keys, nil
		}
		return nil, nil, fmt.Errorf("read planned-workouts dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".icu.md") {
			continue
		}
		path := filepath.Join(dir, name)
		day, err := plannedio.ReadDay(path)
		if err != nil {
			// Tolerate parse errors here: the push step would have
			// already failed loudly. Skip silently to keep the pull
			// resilient.
			continue
		}
		if day.Date == "" || !inRange(day.Date, r) {
			continue
		}
		for _, w := range day.Workouts {
			if w.Meta.IcuEventID != nil && *w.Meta.IcuEventID != 0 {
				ids[*w.Meta.IcuEventID] = true
			}
			keys[day.Date+"|"+w.Meta.Name] = true
		}
	}
	return ids, keys, nil
}

// PruneStalePulled walks the planned-workouts directory and deletes
// any *.icu.md whose embedded id is not in retainID and whose date is
// in range.
//
// File names follow the layout convention
// `YYYY-MM-DD.<icu-id>.icu.md`; pruning is a no-op for any file that
// does not match the pattern.
func PruneStalePulled(dir string, r daterange.Range, retain map[int64]bool, dryRun bool, logf func(string, ...any)) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read planned-workouts dir: %w", err)
	}
	var removed int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".icu.md") {
			continue
		}
		date, idStr, ok := ParsePulledFilename(name)
		if !ok {
			continue
		}
		if !inRange(date, r) {
			continue
		}
		var id int64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil || id == 0 {
			continue
		}
		if retain[id] {
			continue
		}
		path := filepath.Join(dir, name)
		if dryRun {
			logf("[dry-run] remove stale %s", name)
			removed++
			continue
		}
		if err := os.Remove(path); err != nil {
			logf("remove %s: %v", path, err)
			continue
		}
		removed++
		logf("removed stale %s", name)
	}
	return removed, nil
}

// ParsePulledFilename splits "2026-05-04.12345.icu.md" into
// ("2026-05-04", "12345", true). Returns ok=false for any other shape.
// When the filename starts with a YYYY-MM-DD prefix but the id segment
// is missing (e.g. "2026-05-04.icu.md"), the date is still returned to
// help callers log which file was rejected, but ok is false.
func ParsePulledFilename(name string) (date, id string, ok bool) {
	if !strings.HasSuffix(name, ".icu.md") {
		return "", "", false
	}
	stem := strings.TrimSuffix(name, ".icu.md")
	if len(stem) < 10 {
		return "", "", false
	}
	if _, err := time.Parse("2006-01-02", stem[:10]); err != nil {
		return "", "", false
	}
	if len(stem) < 12 || stem[10] != '.' {
		return stem[:10], "", false
	}
	return stem[:10], stem[11:], true
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

// WritePulledFile writes body to path atomically, returning whether
// the file changed (true) or already matched the new bytes (false).
// The `generated_at:` frontmatter line is ignored when comparing, so
// regenerating the same content does not register as a change.
func WritePulledFile(path string, body []byte, dryRun bool) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytesEqualIgnoringGenerated(existing, body) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if dryRun {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := workspace.AtomicWrite(path, body, 0); err != nil {
		return false, err
	}
	return true, nil
}

// bytesEqualIgnoringGenerated compares two rendered files but ignores
// any single `  generated_at:` frontmatter line so re-running the
// command does not report churn for unchanged content.
func bytesEqualIgnoringGenerated(a, b []byte) bool {
	return stripGen(a) == stripGen(b)
}

func stripGen(b []byte) string {
	var out strings.Builder
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimLeft(line, " ")
		if strings.HasPrefix(t, "generated_at:") {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// dateNameKey is the join key used to detect locally-authored workouts
// without an icu_event_id stamp.
func dateNameKey(date time.Time, name string) string {
	return date.Format("2006-01-02") + "|" + name
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

func inRange(date string, r daterange.Range) bool {
	if r.Oldest != "" && date < r.Oldest {
		return false
	}
	if r.Newest != "" && date > r.Newest {
		return false
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
