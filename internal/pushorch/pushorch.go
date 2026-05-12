// Package pushorch diffs the agent-authored planned-workouts/*.md
// markdown files against the .cache/events/*.json snapshot of
// intervals.icu, and pushes the resulting create/update/delete actions
// via internal/icu.
//
// The data flow:
//
//	planned-workouts/YYYY-MM-DD.md   ┐
//	  + ```fit-workout``` DSL        │  Plan(...)  →  Plan{ Actions[] }
//	.cache/events/<id>.json (icu)    ┘
//	                                          │
//	                                          ▼
//	                              Apply(...)  → POST/PUT/DELETE
//	                                          │
//	                                          ▼
//	                          rewrite md to stamp returned id
//
// Diff key is (date, name): one icu event per (date, name) pair. If a
// markdown workout has icu_event_id set, the diff prefers id-match over
// date+name match when reconciling against the cache.
package pushorch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jogvan-k/fit-agent/internal/daterange"
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/plannedio"
	"github.com/jogvan-k/fit-agent/internal/workoutdsl"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// Context bundles the dependencies needed by Plan and Apply.
type Context struct {
	Client    *icu.Client
	AthleteID string
	Layout    workspace.Layout
	Location  *time.Location
	DryRun    bool
	Prune     bool
	Logger    func(format string, args ...any)
}

func (c Context) logf(format string, args ...any) {
	if c.Logger != nil {
		c.Logger(format, args...)
	}
}

// ActionKind discriminates the union of push actions.
type ActionKind int

const (
	// ActionCreate creates a new event on intervals.icu from a local
	// workout that has no icu_event_id stamp.
	ActionCreate ActionKind = iota + 1
	// ActionUpdate updates an existing icu event whose body has drifted.
	ActionUpdate
	// ActionDelete deletes an icu event with no corresponding local
	// workout (only when --prune is set).
	ActionDelete
	// ActionUnchanged reports an event whose body matches the local
	// workout exactly.
	ActionUnchanged
	// ActionSkip reports an icu event that would be deleted but --prune
	// is not set.
	ActionSkip
)

// String returns "create" / "update" / etc.
func (a ActionKind) String() string {
	switch a {
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	case ActionDelete:
		return "delete"
	case ActionUnchanged:
		return "unchanged"
	case ActionSkip:
		return "skip"
	default:
		return fmt.Sprintf("kind(%d)", int(a))
	}
}

// Action is one queued operation.
type Action struct {
	Kind ActionKind
	// Source markdown path (empty for cache-only deletes).
	SourcePath string
	// Workout name within the source file (empty for deletes).
	Name string
	// Date (ISO local) the action is for.
	Date string
	// Event we will POST/PUT, or the cached event we will DELETE.
	Event icu.Event
	// Reason describes the diff outcome (for verbose / dry-run output).
	Reason string
}

// Plan computes the set of actions for the given date range.
func Plan(ctx context.Context, c Context, r daterange.Range) ([]Action, error) {
	mdDays, err := readPlannedDays(c.Layout.PlannedWorkoutsDir(), r)
	if err != nil {
		return nil, err
	}
	cached, err := readCachedEvents(c.Layout.CacheEventsDir(), r)
	if err != nil {
		return nil, err
	}
	// Index cached events by id and by (date, name).
	byID := map[int64]*icu.Event{}
	byKey := map[string]*icu.Event{}
	for i := range cached {
		ev := &cached[i]
		byID[ev.ID] = ev
		byKey[eventKey(ev.StartDateLocal, ev.Name)] = ev
	}
	seenID := map[int64]bool{}
	var actions []Action
	for _, day := range mdDays {
		for _, w := range day.Workouts {
			ev, err := buildEvent(day.Date, w)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", day.Path, w.Meta.Name, err)
			}
			var match *icu.Event
			switch {
			case w.Meta.IcuEventID != nil && *w.Meta.IcuEventID != 0:
				match = byID[*w.Meta.IcuEventID]
				if match == nil {
					// id stamped in md but missing from cache: try to
					// recover by date+name match to avoid creating a
					// duplicate when the cache was cleared.
					match = byKey[eventKey(day.Date+"T00:00:00", w.Meta.Name)]
					if match != nil {
						c.logf("%s/%s: stamped id %d not found in cache, recovered by date+name match (id=%d)",
							day.Path, w.Meta.Name, *w.Meta.IcuEventID, match.ID)
					} else {
						c.logf("%s/%s: stamped id %d not found in cache, will create",
							day.Path, w.Meta.Name, *w.Meta.IcuEventID)
					}
				}
			default:
				match = byKey[eventKey(day.Date+"T00:00:00", w.Meta.Name)]
			}
			if match == nil {
				actions = append(actions, Action{
					Kind:       ActionCreate,
					SourcePath: day.Path,
					Name:       w.Meta.Name,
					Date:       day.Date,
					Event:      ev,
					Reason:     "no matching icu event",
				})
				continue
			}
			seenID[match.ID] = true
			ev.ID = match.ID
			if eventsEqual(*match, ev) {
				actions = append(actions, Action{
					Kind:       ActionUnchanged,
					SourcePath: day.Path,
					Name:       w.Meta.Name,
					Date:       day.Date,
					Event:      ev,
				})
				continue
			}
			actions = append(actions, Action{
				Kind:       ActionUpdate,
				SourcePath: day.Path,
				Name:       w.Meta.Name,
				Date:       day.Date,
				Event:      ev,
				Reason:     diffReason(*match, ev),
			})
		}
	}
	// Cached events not seen in markdown become deletes (or skips
	// without --prune).
	for _, ev := range cached {
		if seenID[ev.ID] {
			continue
		}
		kind := ActionSkip
		reason := "cache-only event; pass --prune to delete"
		if c.Prune {
			kind = ActionDelete
			reason = "missing from markdown"
		}
		actions = append(actions, Action{
			Kind:   kind,
			Date:   strings.SplitN(ev.StartDateLocal, "T", 2)[0],
			Event:  ev,
			Reason: reason,
		})
	}
	sort.SliceStable(actions, func(i, j int) bool {
		if actions[i].Date != actions[j].Date {
			return actions[i].Date < actions[j].Date
		}
		return actions[i].Name < actions[j].Name
	})
	return actions, nil
}

// Apply runs the actions against intervals.icu. Stamping returned IDs
// back into the markdown frontmatter is done in-place.
func Apply(ctx context.Context, c Context, actions []Action) error {
	for _, act := range actions {
		switch act.Kind {
		case ActionCreate:
			if c.DryRun {
				c.logf("[dry-run] CREATE %s %s\n  description:\n    %s",
					act.Date, act.Name, indentBlock(act.Event.Description, "    "))
				continue
			}
			created, err := c.Client.CreateEvent(ctx, c.AthleteID, act.Event)
			if err != nil {
				return fmt.Errorf("create %s/%s: %w", act.Date, act.Name, err)
			}
			c.logf("created %s %s -> id=%d", act.Date, act.Name, created.ID)
			if act.SourcePath != "" {
				if err := stampIDInFile(act.SourcePath, act.Name, created.ID); err != nil {
					return fmt.Errorf("stamp id into %s: %w", act.SourcePath, err)
				}
			}
		case ActionUpdate:
			if c.DryRun {
				c.logf("[dry-run] UPDATE %s %s (id=%d) %s\n  description:\n    %s",
					act.Date, act.Name, act.Event.ID, act.Reason,
					indentBlock(act.Event.Description, "    "))
				continue
			}
			if _, err := c.Client.UpdateEvent(ctx, c.AthleteID, act.Event); err != nil {
				return fmt.Errorf("update %s/%s (id=%d): %w", act.Date, act.Name, act.Event.ID, err)
			}
			c.logf("updated %s %s (id=%d)", act.Date, act.Name, act.Event.ID)
		case ActionDelete:
			if c.DryRun {
				c.logf("[dry-run] DELETE %s id=%d", act.Date, act.Event.ID)
				continue
			}
			if err := c.Client.DeleteEvent(ctx, c.AthleteID, act.Event.ID); err != nil {
				return fmt.Errorf("delete id=%d: %w", act.Event.ID, err)
			}
			c.logf("deleted id=%d (%s)", act.Event.ID, act.Date)
		case ActionSkip:
			c.logf("skip id=%d %s (%s)", act.Event.ID, act.Date, act.Reason)
		case ActionUnchanged:
			// no-op
		}
	}
	return nil
}

// Stats summarises a planned set of actions.
type Stats struct {
	Create, Update, Delete, Unchanged, Skip int
}

// Summarise tallies the actions.
func Summarise(actions []Action) Stats {
	var s Stats
	for _, a := range actions {
		switch a.Kind {
		case ActionCreate:
			s.Create++
		case ActionUpdate:
			s.Update++
		case ActionDelete:
			s.Delete++
		case ActionUnchanged:
			s.Unchanged++
		case ActionSkip:
			s.Skip++
		}
	}
	return s
}

// String renders a one-line summary.
func (s Stats) String() string {
	return fmt.Sprintf("create=%d update=%d delete=%d unchanged=%d skip=%d",
		s.Create, s.Update, s.Delete, s.Unchanged, s.Skip)
}

// readPlannedDays reads every planned-workouts/*.md file whose date
// falls inside the range. Files without a parseable frontmatter date
// are skipped silently (the agent may stash drafts in the dir).
func readPlannedDays(dir string, r daterange.Range) ([]*plannedio.Day, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read planned-workouts dir: %w", err)
	}
	var days []*plannedio.Day
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		day, err := plannedio.ReadDay(path)
		if err != nil {
			return nil, err
		}
		if day.Date == "" {
			continue
		}
		if !inRange(day.Date, r) {
			continue
		}
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return days, nil
}

// readCachedEvents reads every .cache/events/*.json file falling in the
// range.
func readCachedEvents(dir string, r daterange.Range) ([]icu.Event, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cache events dir: %w", err)
	}
	var events []icu.Event
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		buf, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read cached event %s: %w", path, err)
		}
		var ev icu.Event
		if err := json.Unmarshal(buf, &ev); err != nil {
			return nil, fmt.Errorf("decode cached event %s: %w", path, err)
		}
		date := strings.SplitN(ev.StartDateLocal, "T", 2)[0]
		if !inRange(date, r) {
			continue
		}
		events = append(events, ev)
	}
	return events, nil
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

func eventKey(start, name string) string {
	d := strings.SplitN(start, "T", 2)[0]
	return d + "|" + name
}

func eventsEqual(a, b icu.Event) bool {
	return a.Name == b.Name &&
		a.Type == b.Type &&
		a.Description == b.Description &&
		a.MovingTime == b.MovingTime &&
		a.Category == b.Category &&
		startDateMatches(a.StartDateLocal, b.StartDateLocal)
}

// startDateMatches treats "2026-05-04" and "2026-05-04T00:00:00" as
// equivalent (icu always stores the long form, the markdown frontmatter
// only the date).
func startDateMatches(a, b string) bool {
	return strings.SplitN(a, "T", 2)[0] == strings.SplitN(b, "T", 2)[0]
}

func diffReason(have, want icu.Event) string {
	var diffs []string
	if have.Name != want.Name {
		diffs = append(diffs, "name")
	}
	if have.Type != want.Type {
		diffs = append(diffs, "type")
	}
	if have.Description != want.Description {
		diffs = append(diffs, "description")
	}
	if have.MovingTime != want.MovingTime {
		diffs = append(diffs, "moving_time")
	}
	if !startDateMatches(have.StartDateLocal, want.StartDateLocal) {
		diffs = append(diffs, "start_date")
	}
	if len(diffs) == 0 {
		return "metadata changed"
	}
	return "changed: " + strings.Join(diffs, ",")
}

// buildEvent renders the markdown workout into the icu.Event payload
// that we will POST or PUT. If the workout has a fit-workout DSL block
// it is compiled to the ICU description format. When no DSL block is
// present, the description field from the frontmatter YAML is used
// verbatim (useful for workouts — e.g. strength circuits — that cannot
// yet be expressed in the DSL).
func buildEvent(date string, w plannedio.Workout) (icu.Event, error) {
	var desc string
	if w.DSL != "" {
		parsed, err := workoutdsl.Parse(w.DSL)
		if err != nil {
			return icu.Event{}, fmt.Errorf("dsl: %w", err)
		}
		desc = strings.TrimRight(workoutdsl.RenderICU(parsed), "\n")
	} else if w.Meta.Description != "" {
		desc = strings.TrimRight(w.Meta.Description, "\n")
	}
	return icu.Event{
		Category:       "WORKOUT",
		StartDateLocal: date + "T00:00:00",
		Name:           w.Meta.Name,
		Type:           w.Meta.Type,
		Description:    desc,
		MovingTime:     w.Meta.MovingTimeS,
	}, nil
}

// stampIDInFile rewrites a planned-workouts md file in place to record
// a server-assigned event id.
func stampIDInFile(path, name string, id int64) error {
	buf, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := plannedio.StampEventID(string(buf), name, id)
	if err != nil {
		return err
	}
	if out == string(buf) {
		return nil
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// indentBlock prefixes every line after the first with the given indent.
func indentBlock(s, indent string) string {
	return strings.ReplaceAll(s, "\n", "\n"+indent)
}
