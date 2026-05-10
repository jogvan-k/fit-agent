// Package renderorch reads cached intervals.icu payloads + parsed FIT
// files from a workspace's .cache/ tree and produces the agent-facing
// data files described in the plan (§4, §10): per-day activity YAML,
// per-month wellness YAML, per-day planned-workout markdown.
//
// This package owns the orchestration (which days to render, how to
// load and combine cached data); the actual byte emission lives in
// internal/render. Splitting the two keeps render purely functional
// and easy to golden-test, while the orchestration here can be I/O-
// heavy without polluting render's tests.
package renderorch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/fitparse"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/plannedio"
	"github.com/jogvan-k/fit-agent/internal/render"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// Context bundles the workspace layout and athlete timezone every
// render function needs.
type Context struct {
	Layout   workspace.Layout
	Location *time.Location
	// Now is the timestamp stamped into generated_at fields. Defaults
	// to time.Now() when zero.
	Now time.Time
	// DryRun reports outcomes without writing.
	DryRun bool
	Logger func(format string, args ...any)
}

func (c Context) now() time.Time {
	if c.Now.IsZero() {
		return time.Now()
	}
	return c.Now
}

func (c Context) logf(format string, args ...any) {
	if c.Logger != nil {
		c.Logger(format, args...)
	}
}

// Stats mirrors cache.Stats; copied here to avoid an import cycle.
type Stats struct {
	Added     int
	Updated   int
	Unchanged int
	Errors    int
}

// Add increments the counter corresponding to outcome o.
func (s *Stats) Add(o Outcome) {
	switch o {
	case OutcomeAdded:
		s.Added++
	case OutcomeUpdated:
		s.Updated++
	case OutcomeUnchanged:
		s.Unchanged++
	}
}

// Merge folds the counters of o into s.
func (s *Stats) Merge(o Stats) {
	s.Added += o.Added
	s.Updated += o.Updated
	s.Unchanged += o.Unchanged
	s.Errors += o.Errors
}

func (s Stats) String() string {
	return fmt.Sprintf("added=%d updated=%d unchanged=%d errors=%d",
		s.Added, s.Updated, s.Unchanged, s.Errors)
}

// Outcome reports whether a render created, updated, or left a file alone.
type Outcome int

const (
	// OutcomeAdded means the renderer wrote a new file.
	OutcomeAdded Outcome = iota
	// OutcomeUpdated means the renderer rewrote an existing file.
	OutcomeUpdated
	// OutcomeUnchanged means the rendered bytes matched the existing file.
	OutcomeUnchanged
)

// String makes Outcome satisfy fmt.Stringer for human-readable logs.
func (o Outcome) String() string {
	switch o {
	case OutcomeAdded:
		return "added"
	case OutcomeUpdated:
		return "updated"
	case OutcomeUnchanged:
		return "unchanged"
	}
	return "?"
}

// Activities renders all activities cached for the supplied range,
// grouping them by local calendar date.
func Activities(ctx context.Context, c Context, r daterange.Range) (Stats, error) {
	_ = ctx
	var stats Stats
	groups, err := groupCachedActivities(c.Layout, c.Location, r)
	if err != nil {
		return stats, err
	}
	dates := make([]string, 0, len(groups))
	for k := range groups {
		dates = append(dates, k)
	}
	sort.Strings(dates)
	for _, d := range dates {
		date, err := time.ParseInLocation(daterange.DateLayout, d, c.Location)
		if err != nil {
			stats.Errors++
			continue
		}
		body, err := renderActivityDay(c, date, groups[d])
		if err != nil {
			stats.Errors++
			c.logf("activities %s: %v", d, err)
			continue
		}
		out, err := writeRendered(c.Layout.ActivityDayPath(date), body, c.DryRun)
		if err != nil {
			stats.Errors++
			continue
		}
		stats.Add(out)
		c.logf("activities %s: %s", d, out)
	}
	return stats, nil
}

// SingleActivity renders one activity's day file (loading any other
// activities for the same day from the cache so the file is complete).
// It returns the rendered bytes for `render activity --stdout` use cases.
func SingleActivity(ctx context.Context, c Context, id string) ([]byte, Outcome, error) {
	_ = ctx
	jsonPath := c.Layout.CacheActivityJSONPath(id)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", jsonPath, err)
	}
	var s icu.ActivitySummary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, 0, fmt.Errorf("decode %s: %w", jsonPath, err)
	}
	date, err := parseLocalDate(s.StartDateLocal, c.Location)
	if err != nil {
		return nil, 0, fmt.Errorf("activity %s: %w", id, err)
	}
	r := daterange.Range{
		Oldest: date.Format(daterange.DateLayout), Newest: date.Format(daterange.DateLayout),
		OldestT: date, NewestT: date,
	}
	groups, err := groupCachedActivities(c.Layout, c.Location, r)
	if err != nil {
		return nil, 0, err
	}
	day := groups[date.Format(daterange.DateLayout)]
	body, err := renderActivityDay(c, date, day)
	if err != nil {
		return nil, 0, err
	}
	out, err := writeRendered(c.Layout.ActivityDayPath(date), body, c.DryRun)
	if err != nil {
		return body, out, err
	}
	return body, out, nil
}

// Wellness renders one YAML file per calendar month covered by the
// range, reading days from the matching .cache/wellness JSON files.
func Wellness(ctx context.Context, c Context, r daterange.Range) (Stats, error) {
	_ = ctx
	var stats Stats
	for _, month := range r.MonthsCovered() {
		path := c.Layout.CacheWellnessMonthPath(month)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			stats.Errors++
			c.logf("wellness %s: %v", month.Format("2006-01"), err)
			continue
		}
		var days []icu.WellnessDay
		if err := json.Unmarshal(data, &days); err != nil {
			stats.Errors++
			c.logf("wellness %s decode: %v", month.Format("2006-01"), err)
			continue
		}
		body, err := render.WellnessMonthYAML(render.WellnessMonth{
			Month:       month,
			GeneratedAt: c.now(),
			Location:    c.Location,
			Days:        days,
		})
		if err != nil {
			stats.Errors++
			continue
		}
		out, err := writeRendered(c.Layout.WellnessMonthPath(month), body, c.DryRun)
		if err != nil {
			stats.Errors++
			continue
		}
		stats.Add(out)
		c.logf("wellness %s: %s", month.Format("2006-01"), out)
	}
	return stats, nil
}

// Planned reconciles cached planned-workout events against the
// workspace's planned-workouts directory.
//
// Each `planned-workouts/YYYY-MM-DD.md` is a jointly-owned file: the
// agent owns the frontmatter (with the `workouts:` list, `name`, and
// `icu_event_id` stamps), the prose, and the ```fit-workout``` fences;
// the CLI owns a single fenced YAML block delimited by
// `<!-- fit-agent:icu:begin -->` ... `<!-- fit-agent:icu:end -->`
// sentinels.
//
// Planned:
//
//   - Groups every `.cache/events/<id>.json` in range by local
//     calendar date.
//   - For each date with at least one event, renders the machine
//     block via [render.ICUBlock] containing every icu event for
//     that date and splices it into `<date>.md` via
//     [plannedio.SpliceICUBlock]. Bytes outside the sentinels are
//     preserved verbatim.
//   - If `<date>.md` does not yet exist, the file is created with the
//     default agent-owned skeleton ([render.EmptyPlannedDay]) and the
//     freshly-rendered block.
//
// A date whose only icu events have been deleted on the icu side
// disappears from the cache; the local `<date>.md` retains its prior
// machine block until the agent removes the file or until a later
// `render planned` run with at least one event for that date rewrites
// the block. This matches the joint-ownership contract: the CLI does
// not delete agent-owned files.
func Planned(ctx context.Context, c Context, r daterange.Range) (Stats, error) {
	_ = ctx
	var stats Stats
	matches, err := filepath.Glob(filepath.Join(c.Layout.CacheEventsDir(), "*.json"))
	if err != nil {
		return stats, err
	}

	// Group cached events by local calendar date.
	byDate := map[string][]icu.Event{}
	dateTimes := map[string]time.Time{}
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			stats.Errors++
			c.logf("planned %s: read: %v", filepath.Base(p), err)
			continue
		}
		var ev icu.Event
		if err := json.Unmarshal(data, &ev); err != nil {
			stats.Errors++
			c.logf("planned %s: decode: %v", filepath.Base(p), err)
			continue
		}
		date, err := parseLocalDate(ev.StartDateLocal, c.Location)
		if err != nil {
			stats.Errors++
			c.logf("planned %s: parse start_date_local %q: %v", filepath.Base(p), ev.StartDateLocal, err)
			continue
		}
		if date.Before(r.OldestT) || date.After(r.NewestT) {
			continue
		}
		key := date.Format(daterange.DateLayout)
		byDate[key] = append(byDate[key], ev)
		dateTimes[key] = date
	}

	// Process dates in deterministic order so logs read top-to-bottom.
	keys := make([]string, 0, len(byDate))
	for k := range byDate {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		date := dateTimes[k]
		events := byDate[k]
		out, err := writePlannedDay(c, date, events)
		if err != nil {
			stats.Errors++
			c.logf("planned %s: %v", k, err)
			continue
		}
		stats.Add(out)
		c.logf("planned %s: %s (events=%d)", k, out, len(events))
	}
	return stats, nil
}

// writePlannedDay renders the machine block for date and splices it
// into the corresponding planned-workouts file, creating a default
// skeleton when none exists.
func writePlannedDay(c Context, date time.Time, events []icu.Event) (Outcome, error) {
	path := c.Layout.PlannedWorkoutDayPath(date)
	block := render.ICUBlock(render.ICUBlockInput{
		Date:        date,
		GeneratedAt: c.now(),
		Events:      events,
	})

	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		// File exists; splice the block in place.
	case errors.Is(err, os.ErrNotExist):
		existing = render.EmptyPlannedDay(date)
	default:
		return 0, fmt.Errorf("read %s: %w", path, err)
	}

	updated := plannedio.SpliceICUBlock(existing, block)
	return writeRendered(path, updated, c.DryRun)
}

// All runs Activities + Wellness + Planned for the range.
func All(ctx context.Context, c Context, r daterange.Range) (Stats, error) {
	var combined Stats
	if s, err := Activities(ctx, c, r); err != nil {
		combined.Errors++
		c.logf("activities: %v", err)
	} else {
		combined.Merge(s)
	}
	if s, err := Wellness(ctx, c, r); err != nil {
		combined.Errors++
		c.logf("wellness: %v", err)
	} else {
		combined.Merge(s)
	}
	if s, err := Planned(ctx, c, r); err != nil {
		combined.Errors++
		c.logf("planned: %v", err)
	} else {
		combined.Merge(s)
	}
	return combined, nil
}

// renderActivityDay assembles a render.ActivityDay from the cached
// per-activity inputs and emits the YAML.
func renderActivityDay(c Context, date time.Time, inputs []render.ActivityInput) ([]byte, error) {
	return render.ActivityDayYAML(render.ActivityDay{
		Date:        date,
		GeneratedAt: c.now(),
		Location:    c.Location,
		Activities:  inputs,
	})
}

// groupCachedActivities walks .cache/activities/, decodes each JSON,
// (optionally) parses the matching FIT file, and groups by local
// calendar date.
func groupCachedActivities(l workspace.Layout, loc *time.Location, r daterange.Range) (map[string][]render.ActivityInput, error) {
	matches, err := filepath.Glob(filepath.Join(l.CacheActivitiesDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	out := map[string][]render.ActivityInput{}
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s icu.ActivitySummary
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		date, err := parseLocalDate(s.StartDateLocal, loc)
		if err != nil {
			continue
		}
		if date.Before(r.OldestT) || date.After(r.NewestT) {
			continue
		}
		input := render.ActivityInput{Summary: s}
		fitPath := l.CacheActivityFITPath(s.ID)
		if _, err := os.Stat(fitPath); err == nil {
			if parsed, perr := fitparse.Decode(fitPath); perr == nil {
				input.FIT = parsed
			}
		}
		key := date.Format(daterange.DateLayout)
		out[key] = append(out[key], input)
	}
	return out, nil
}

// parseLocalDate accepts the start_date_local strings intervals.icu
// emits ("2026-05-10T08:00:00") and returns the local calendar date.
func parseLocalDate(s string, loc *time.Location) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty start_date_local")
	}
	// Truncate any "Z" or "+HH:MM" suffix; intervals.icu's
	// start_date_local is wall-clock and shouldn't carry an offset.
	s = strings.SplitN(s, "Z", 2)[0]
	if i := strings.IndexAny(s[10:], "+-"); i > 0 {
		s = s[:10+i]
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", s, loc)
	if err != nil {
		// Fall back to date-only.
		t, err = time.ParseInLocation(daterange.DateLayout, s[:min(10, len(s))], loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse %q: %w", s, err)
		}
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc), nil
}

// writeRendered is the same hash-then-write helper the cache package
// uses, but lives here to avoid an import cycle.
//
// The "logical" comparison strips the `generated_at:` line so a
// re-render with a fresh timestamp does not show as updated when the
// underlying data is identical (idempotency requirement, plan §15).
func writeRendered(path string, data []byte, dryRun bool) (Outcome, error) {
	existing, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}
	var outcome Outcome
	switch {
	case !exists:
		outcome = OutcomeAdded
	case sum(stripGeneratedAt(existing)) == sum(stripGeneratedAt(data)):
		outcome = OutcomeUnchanged
	default:
		outcome = OutcomeUpdated
	}
	if dryRun || outcome == OutcomeUnchanged {
		return outcome, nil
	}
	if err := workspace.AtomicWrite(path, data, 0); err != nil {
		return 0, err
	}
	return outcome, nil
}

// stripGeneratedAt removes any `generated_at: ...` line from the
// rendered output so the idempotency check ignores the timestamp.
func stripGeneratedAt(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for _, line := range bytes.Split(b, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimLeft(line, " "), []byte("generated_at:")) {
			continue
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return out
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
