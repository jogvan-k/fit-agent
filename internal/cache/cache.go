// Package cache writes raw intervals.icu payloads (JSON for athlete /
// activity-list / activity / wellness / events; binary FIT for
// activities) into the workspace's .cache/ tree.
//
// Every cache function:
//
//   - Reads from icu via [icu.Client] using raw-JSON variants so the
//     bytes are stored verbatim (the renderer is the typed consumer).
//   - Writes through [workspace.AtomicWrite] so partial writes are
//     never visible.
//   - Returns a [Stats] summary so the CLI can print added/updated/
//     unchanged counts.
//
// Functions take a [Context] bundle (icu client + workspace layout +
// timezone) so the CLI never has to thread three args.
package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// Context bundles the resolved icu client, workspace layout, and
// athlete timezone. CLI commands construct it from a *runtime.Resolved.
type Context struct {
	Client    *icu.Client
	Layout    workspace.Layout
	AthleteID string
	Location  *time.Location
	// DryRun, when true, skips all disk writes and network requests
	// for FIT downloads but DOES make read-only list calls to icu so
	// the dry-run summary reflects what would change.
	DryRun bool
	// ForceRefit, when true, re-downloads FIT files even when the
	// cache copy is present.
	ForceRefit bool
	// Logger is invoked once per cache operation; nil disables
	// progress logging.
	Logger func(format string, args ...any)
}

func (c Context) logf(format string, args ...any) {
	if c.Logger != nil {
		c.Logger(format, args...)
	}
}

// Stats reports counts for one cache run.
type Stats struct {
	Added     int
	Updated   int
	Unchanged int
	Skipped   int
	Errors    int
}

// Add merges one entry's outcome into the stats.
func (s *Stats) Add(o Outcome) {
	switch o {
	case OutcomeAdded:
		s.Added++
	case OutcomeUpdated:
		s.Updated++
	case OutcomeUnchanged:
		s.Unchanged++
	case OutcomeSkipped:
		s.Skipped++
	}
}

// Merge folds another Stats into the receiver.
func (s *Stats) Merge(o Stats) {
	s.Added += o.Added
	s.Updated += o.Updated
	s.Unchanged += o.Unchanged
	s.Skipped += o.Skipped
	s.Errors += o.Errors
}

// String formats the stats for a one-line CLI summary.
func (s Stats) String() string {
	return fmt.Sprintf("added=%d updated=%d unchanged=%d skipped=%d errors=%d",
		s.Added, s.Updated, s.Unchanged, s.Skipped, s.Errors)
}

// Outcome is the result of writing a single cache file.
type Outcome int

const (
	// OutcomeAdded means the file did not exist before.
	OutcomeAdded Outcome = iota
	// OutcomeUpdated means the file existed and the new content
	// differed from disk.
	OutcomeUpdated
	// OutcomeUnchanged means the file existed and matched the new
	// content byte-for-byte.
	OutcomeUnchanged
	// OutcomeSkipped means the cache function chose not to fetch
	// (e.g. FIT file already cached without --force-refit).
	OutcomeSkipped
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
	case OutcomeSkipped:
		return "skipped"
	}
	return "?"
}

// Athlete fetches /athlete/0 and writes .cache/athlete.json.
func Athlete(ctx context.Context, c Context) (Outcome, error) {
	raw, err := c.Client.GetAthleteRaw(ctx, icu.SelfAthleteID)
	if err != nil {
		return 0, fmt.Errorf("get athlete: %w", err)
	}
	pretty, err := prettify(raw)
	if err != nil {
		return 0, err
	}
	out, err := writeCache(c.Layout.CacheAthletePath(), pretty, c.DryRun)
	if err == nil {
		c.logf("athlete: %s %s", out, c.Layout.CacheAthletePath())
	}
	return out, err
}

// Wellness fetches /athlete/{id}/wellness for the range and writes one
// JSON file per calendar month covered by the range.
//
// Re-fetches always overwrite (the API may amend past days as wearables
// sync); the file's content hash is what determines added vs updated
// vs unchanged.
func Wellness(ctx context.Context, c Context, r daterange.Range) (Stats, error) {
	var stats Stats
	for _, month := range r.MonthsCovered() {
		oldest, newest := monthRange(month, r)
		raw, err := c.Client.ListWellnessRaw(ctx, c.AthleteID, oldest, newest)
		if err != nil {
			c.logf("wellness %s: error %v", month.Format("2006-01"), err)
			stats.Errors++
			continue
		}
		// Merge with any existing month file so that fetching a
		// 7-day range does not blow away the rest of the month.
		merged, err := mergeWellnessMonth(c.Layout.CacheWellnessMonthPath(month), raw)
		if err != nil {
			stats.Errors++
			continue
		}
		out, err := writeCache(c.Layout.CacheWellnessMonthPath(month), merged, c.DryRun)
		if err != nil {
			stats.Errors++
			continue
		}
		stats.Add(out)
		c.logf("wellness %s: %s", month.Format("2006-01"), out)
	}
	return stats, nil
}

// Events fetches /athlete/{id}/events for the range and writes one
// .cache/events/<id>.json per planned-workout event returned.
func Events(ctx context.Context, c Context, r daterange.Range) (Stats, error) {
	var stats Stats
	raw, err := c.Client.ListEventsRaw(ctx, c.AthleteID, r.Oldest, r.Newest, "WORKOUT")
	if err != nil {
		return stats, fmt.Errorf("list events: %w", err)
	}
	var events []json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil {
		return stats, fmt.Errorf("decode events array: %w", err)
	}
	for _, ev := range events {
		var meta struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(ev, &meta); err != nil || meta.ID == 0 {
			stats.Errors++
			continue
		}
		idStr := fmt.Sprintf("%d", meta.ID)
		pretty, err := prettify(ev)
		if err != nil {
			stats.Errors++
			continue
		}
		out, err := writeCache(c.Layout.CacheEventPath(idStr), pretty, c.DryRun)
		if err != nil {
			stats.Errors++
			continue
		}
		stats.Add(out)
		c.logf("event %s: %s", idStr, out)
	}
	return stats, nil
}

// Activities fetches the activity list for the range and, for each
// activity, writes a JSON file and (when an associated FIT file
// exists) a .fit binary alongside.
//
// FIT files are only re-downloaded when [Context.ForceRefit] is true
// or when the local FIT does not exist; the JSON metadata is always
// fetched anew.
func Activities(ctx context.Context, c Context, r daterange.Range) (Stats, error) {
	var stats Stats
	list, err := c.Client.ListActivities(ctx, c.AthleteID, r.Oldest, r.Newest)
	if err != nil {
		return stats, fmt.Errorf("list activities: %w", err)
	}
	for _, a := range list {
		if a.ID == "" {
			stats.Errors++
			continue
		}
		// JSON metadata: always re-fetch the per-activity detail
		// payload (the list endpoint omits some fields).
		raw, err := c.Client.GetActivityRaw(ctx, a.ID)
		if err != nil {
			c.logf("activity %s json: error %v", a.ID, err)
			stats.Errors++
			continue
		}
		pretty, err := prettify(raw)
		if err != nil {
			stats.Errors++
			continue
		}
		jsonOut, err := writeCache(c.Layout.CacheActivityJSONPath(a.ID), pretty, c.DryRun)
		if err != nil {
			stats.Errors++
			continue
		}
		stats.Add(jsonOut)
		c.logf("activity %s json: %s", a.ID, jsonOut)

		// FIT binary: skip when not present on the icu side.
		if a.FileType != "fit" {
			continue
		}
		fitPath := c.Layout.CacheActivityFITPath(a.ID)
		if !c.ForceRefit {
			if _, err := os.Stat(fitPath); err == nil {
				stats.Add(OutcomeSkipped)
				c.logf("activity %s fit: cached", a.ID)
				continue
			}
		}
		if c.DryRun {
			stats.Add(OutcomeAdded)
			c.logf("activity %s fit: would download", a.ID)
			continue
		}
		var buf bytes.Buffer
		if err := c.Client.GetActivityFIT(ctx, a.ID, &buf); err != nil {
			c.logf("activity %s fit: error %v", a.ID, err)
			stats.Errors++
			continue
		}
		fitOut, err := writeCache(fitPath, buf.Bytes(), c.DryRun)
		if err != nil {
			stats.Errors++
			continue
		}
		stats.Add(fitOut)
		c.logf("activity %s fit: %s", a.ID, fitOut)
	}
	return stats, nil
}

// SingleActivity fetches one activity (JSON + FIT when available)
// regardless of any range. Used by `cache activity <id>`.
func SingleActivity(ctx context.Context, c Context, id string) (Stats, error) {
	var stats Stats
	raw, err := c.Client.GetActivityRaw(ctx, id)
	if err != nil {
		return stats, fmt.Errorf("get activity %s: %w", id, err)
	}
	pretty, err := prettify(raw)
	if err != nil {
		return stats, err
	}
	jsonOut, err := writeCache(c.Layout.CacheActivityJSONPath(id), pretty, c.DryRun)
	if err != nil {
		return stats, err
	}
	stats.Add(jsonOut)
	c.logf("activity %s json: %s", id, jsonOut)

	// Inspect the cached JSON to discover file_type.
	var meta struct {
		FileType string `json:"file_type"`
	}
	_ = json.Unmarshal(raw, &meta)
	if meta.FileType != "fit" {
		return stats, nil
	}
	fitPath := c.Layout.CacheActivityFITPath(id)
	if !c.ForceRefit {
		if _, err := os.Stat(fitPath); err == nil {
			stats.Add(OutcomeSkipped)
			return stats, nil
		}
	}
	if c.DryRun {
		stats.Add(OutcomeAdded)
		return stats, nil
	}
	var buf bytes.Buffer
	if err := c.Client.GetActivityFIT(ctx, id, &buf); err != nil {
		return stats, fmt.Errorf("download fit %s: %w", id, err)
	}
	fitOut, err := writeCache(fitPath, buf.Bytes(), c.DryRun)
	if err != nil {
		return stats, err
	}
	stats.Add(fitOut)
	c.logf("activity %s fit: %s", id, fitOut)
	return stats, nil
}

// All runs Athlete + Activities + Wellness + Events for the range.
// Errors from individual sections are reported in the per-section
// Stats.Errors counter; the function returns nil unless something
// catastrophic happens (e.g. icu is unreachable for the whole run).
func All(ctx context.Context, c Context, r daterange.Range) (Stats, error) {
	var combined Stats
	out, err := Athlete(ctx, c)
	if err != nil {
		combined.Errors++
		c.logf("athlete: %v", err)
	} else {
		combined.Add(out)
	}
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
	if s, err := Events(ctx, c, r); err != nil {
		combined.Errors++
		c.logf("events: %v", err)
	} else {
		combined.Merge(s)
	}
	return combined, nil
}

// writeCache writes data to path atomically and returns whether the
// file was added, updated, or unchanged. In dry-run mode, no I/O
// occurs and the comparison is still performed against the existing
// on-disk file (if any) so the reported outcome is realistic.
func writeCache(path string, data []byte, dryRun bool) (Outcome, error) {
	existing, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}
	var outcome Outcome
	switch {
	case !exists:
		outcome = OutcomeAdded
	case sha256bytes(existing) == sha256bytes(data):
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

func sha256bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// prettify pretty-prints JSON for stable diffs in the cache.
func prettify(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// mergeWellnessMonth merges fresh wellness rows into any existing
// month file so a 7-day fetch does not erase earlier days. Both
// inputs are arrays of WellnessDay JSON objects keyed by id (date
// string). Fresh days replace existing ones with the same id.
func mergeWellnessMonth(path string, fresh []byte) ([]byte, error) {
	var freshDays []json.RawMessage
	if err := json.Unmarshal(fresh, &freshDays); err != nil {
		return nil, fmt.Errorf("decode fresh wellness: %w", err)
	}
	idx := map[string]json.RawMessage{}
	for _, d := range freshDays {
		var meta struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(d, &meta); err == nil && meta.ID != "" {
			idx[meta.ID] = d
		}
	}
	if existing, err := os.ReadFile(path); err == nil {
		var prev []json.RawMessage
		if err := json.Unmarshal(existing, &prev); err == nil {
			for _, d := range prev {
				var meta struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(d, &meta); err != nil || meta.ID == "" {
					continue
				}
				if _, override := idx[meta.ID]; !override {
					idx[meta.ID] = d
				}
			}
		}
	}
	keys := make([]string, 0, len(idx))
	for k := range idx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]json.RawMessage, len(keys))
	for i, k := range keys {
		out[i] = idx[k]
	}
	merged, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return prettify(merged)
}

// monthRange returns the YYYY-MM-DD oldest/newest pair for fetching a
// single month, clipped to the user's overall range.
func monthRange(month time.Time, overall daterange.Range) (oldest, newest string) {
	loc := month.Location()
	mStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, loc)
	mEnd := mStart.AddDate(0, 1, -1)
	if mStart.Before(overall.OldestT) {
		mStart = overall.OldestT
	}
	if mEnd.After(overall.NewestT) {
		mEnd = overall.NewestT
	}
	return mStart.Format(daterange.DateLayout), mEnd.Format(daterange.DateLayout)
}

// Discard is an [io.Writer] that drops everything; exposed so test
// helpers can pass a logger that swallows output.
var Discard io.Writer = io.Discard
