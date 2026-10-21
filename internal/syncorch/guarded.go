package syncorch

import (
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
	"github.com/jogvan-k/fit-agent/internal/icu"
	"github.com/jogvan-k/fit-agent/internal/plannedio"
	"github.com/jogvan-k/fit-agent/internal/pushorch"
	"github.com/jogvan-k/fit-agent/internal/workspace"
)

// ErrStalePreview means the approved plan no longer matches the current state.
var ErrStalePreview = errors.New("stale preview; preview again and confirm the new plan")

// ErrConflict means a remote change needs manual reconciliation.
var ErrConflict = errors.New("sync conflict; reconcile local and remote workouts")

// SafeAction is a content-free calendar diff suitable for agent tool output.
type SafeAction struct {
	Kind    string `json:"kind"`
	Date    string `json:"date"`
	EventID int64  `json:"eventId"`
}

// GuardedResult is the version-one machine sync result. Fingerprint binds the
// CLI version, identity, range, local bytes, baseline, and live remote inventory.
type GuardedResult struct {
	From        string       `json:"from"`
	To          string       `json:"to"`
	DryRun      bool         `json:"dryRun"`
	Fingerprint string       `json:"fingerprint"`
	Actions     []SafeAction `json:"actions"`
}

// GuardedSync uses the existing planner, applier and pull renderer. Unlike the
// legacy path it preserves the baseline until conflict and approval checks pass.
// No local or remote writes occur during preview or rejected confirmation.
func GuardedSync(ctx context.Context, c Context, r daterange.Range, identity, expected string) (GuardedResult, error) {
	res := GuardedResult{From: r.Oldest, To: r.Newest, DryRun: c.DryRun, Actions: []SafeAction{}}
	start, err := time.Parse("2006-01-02", r.Oldest)
	if err != nil {
		return res, fmt.Errorf("validation: bounded start date required: %w", err)
	}
	end, err := time.Parse("2006-01-02", r.Newest)
	if err != nil {
		return res, fmt.Errorf("validation: bounded end date required: %w", err)
	}
	if end.Before(start) || end.Sub(start) >= 90*24*time.Hour {
		return res, fmt.Errorf("validation: range must contain 1 to 90 calendar days")
	}
	before, err := plannedDigest(c.Layout.PlannedWorkoutsDir())
	if err != nil {
		return res, err
	}
	live, err := c.Client.ListEvents(ctx, c.AthleteID, r.Oldest, r.Newest, icu.EventCategoryWorkout)
	if err != nil {
		return res, fmt.Errorf("list events: %w", err)
	}
	if live == nil {
		live = []icu.Event{}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	keys := map[string]bool{}
	ids := map[int64]bool{}
	for _, ev := range live {
		date := strings.SplitN(ev.StartDateLocal, "T", 2)[0]
		if _, err := time.Parse("2006-01-02", date); err != nil || date < r.Oldest || date > r.Newest || ev.ID <= 0 {
			return res, fmt.Errorf("validation: remote inventory contains invalid or out-of-range events")
		}
		key := date + "\x00" + ev.Name
		if keys[key] || ids[ev.ID] {
			return res, ErrConflict
		}
		keys[key] = true
		ids[ev.ID] = true
	}
	pctx := pushorch.Context{Client: c.Client, AthleteID: c.AthleteID, Layout: c.Layout,
		Location: c.Location, DryRun: c.DryRun, Prune: c.Prune, Inventory: live}
	actions, err := pushorch.Plan(ctx, pctx, r)
	if err != nil {
		return res, fmt.Errorf("plan push: %w", err)
	}
	if len(actions) > 500 {
		return res, fmt.Errorf("validation: too many sync actions (maximum 500)")
	}
	keys = map[string]bool{}
	ids = map[int64]bool{}
	for _, a := range actions {
		key := a.Date + "\x00" + a.Name
		if a.SourcePath == "" {
			continue
		}
		if keys[key] || (a.Event.ID != 0 && ids[a.Event.ID]) {
			return res, ErrConflict
		}
		keys[key] = true
		ids[a.Event.ID] = true
	}
	// Compare mutation candidates with the last confirmed sync baseline. Missing
	// baselines are ambiguous: never overwrite an unobserved remote workout.
	baseline := make(map[int64]json.RawMessage)
	conflict := false
	for i := range actions {
		a := &actions[i]
		if a.Kind != pushorch.ActionUpdate && a.Kind != pushorch.ActionDelete {
			continue
		}
		data, readErr := os.ReadFile(c.Layout.SyncBaselinePath(fmt.Sprint(a.Event.ID)))
		var old icu.Event
		if readErr != nil && !os.IsNotExist(readErr) {
			return res, fmt.Errorf("read baseline: %w", readErr)
		}
		var remote icu.Event
		for _, ev := range live {
			if ev.ID == a.Event.ID {
				remote = ev
				break
			}
		}
		if readErr != nil || json.Unmarshal(data, &old) != nil || !sameWorkout(old, remote) {
			a.Kind = pushorch.ActionConflict
			conflict = true
		}
		if json.Valid(data) {
			baseline[a.Event.ID] = data
		}
	}
	// Intervals.icu's event endpoints currently offer no revision/ETag
	// precondition that can bind a PUT or DELETE to the inventory inspected
	// above. A remote edit made after ListEvents and before an unconditional
	// mutation would otherwise be silently overwritten. The machine protocol is
	// deliberately stricter than the legacy human CLI: it can create new events,
	// but fails closed for changes to existing remote events until the upstream
	// API exposes a conditional-write primitive.
	for i := range actions {
		a := &actions[i]
		if a.Kind == pushorch.ActionUpdate || a.Kind == pushorch.ActionDelete {
			a.Kind = pushorch.ActionConflict
			conflict = true
		}
	}
	// A stamped event deleted or moved remotely is also a conflict. The legacy
	// planner proposes create in this case; only an unstamped workout is new.
	for i := range actions {
		a := &actions[i]
		if a.Kind != pushorch.ActionCreate {
			continue
		}
		stamped, stampErr := hasStampedWorkout(a.SourcePath, a.Name)
		if stampErr != nil {
			return res, stampErr
		}
		if stamped {
			a.Kind = pushorch.ActionConflict
			conflict = true
		}
	}
	after, err := plannedDigest(c.Layout.PlannedWorkoutsDir())
	if err != nil {
		return res, err
	}
	if before != after {
		return res, ErrStalePreview
	}
	payload, err := json.Marshal([]any{1, identity, c.Layout.Root, c.AthleteID, r.Oldest, r.Newest, c.Prune, before, baseline, live, actions})
	if err != nil {
		return res, fmt.Errorf("fingerprint plan: %w", err)
	}
	sum := sha256.Sum256(payload)
	res.Fingerprint = hex.EncodeToString(sum[:])
	for _, a := range actions {
		res.Actions = append(res.Actions, SafeAction{Kind: a.Kind.String(), Date: a.Date, EventID: a.Event.ID})
	}
	if c.DryRun {
		return res, nil
	}
	if expected == "" || expected != res.Fingerprint {
		return res, ErrStalePreview
	}
	if conflict {
		return res, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	// Recheck immediately before applying the in-memory plan, including changes
	// made while hashing a large inventory. Apply never replans from disk.
	current, err := plannedDigest(c.Layout.PlannedWorkoutsDir())
	if err != nil {
		return res, err
	}
	if current != before {
		return res, ErrStalePreview
	}
	if err := pushorch.Apply(ctx, pctx, actions); err != nil {
		return res, fmt.Errorf("apply push: %w", err)
	}
	for i, a := range actions {
		res.Actions[i].EventID = a.Event.ID
	}
	// Only an explicitly confirmed, successful sync establishes a baseline.
	// Unchanged events can be adopted after manual local/remote reconciliation.
	for _, a := range actions {
		if a.Kind != pushorch.ActionCreate && a.Kind != pushorch.ActionUpdate && a.Kind != pushorch.ActionUnchanged {
			continue
		}
		ev := a.Event
		if a.Kind == pushorch.ActionUnchanged {
			for _, remote := range live {
				if remote.ID == ev.ID {
					ev = remote
					break
				}
			}
		}
		data, err := json.Marshal(ev)
		if err != nil {
			return res, fmt.Errorf("encode sync baseline: %w", err)
		}
		if err := workspace.AtomicWrite(c.Layout.SyncBaselinePath(fmt.Sprint(ev.ID)), data, 0o644); err != nil {
			return res, fmt.Errorf("write sync baseline: %w", err)
		}
	}
	stats, err := pull(ctx, c, r, true)
	if err != nil {
		return res, err
	}
	if stats.Errors > 0 || stats.Render.Errors > 0 {
		return res, fmt.Errorf("sync: pull completed with errors")
	}
	return res, nil
}

func sameWorkout(a, b icu.Event) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Type == b.Type && a.Category == b.Category &&
		a.StartDateLocal == b.StartDateLocal && a.Description == b.Description && a.MovingTime == b.MovingTime &&
		string(a.WorkoutDoc) == string(b.WorkoutDoc)
}

func hasStampedWorkout(path, name string) (bool, error) {
	day, err := plannedio.ReadDay(path)
	if err != nil {
		return false, err
	}
	for _, w := range day.Workouts {
		if w.Meta.Name == name {
			return w.Meta.IcuEventID != nil && *w.Meta.IcuEventID != 0, nil
		}
	}
	return false, ErrStalePreview
}

// All markdown files are hashed because the planner selects by frontmatter,
// not filenames. This deliberately errs toward invalidating an old preview.
func plannedDigest(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read planned directory: %w", err)
	}
	h := sha256.New()
	var size int64
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("validation: planned files must be regular files")
		}
		size += info.Size()
		if size > 8<<20 {
			return "", fmt.Errorf("validation: planned files exceed 8 MiB")
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return "", err
		}
		name, _ := json.Marshal(e.Name())
		content := sha256.Sum256(data)
		fmt.Fprintf(h, "%s:%x\n", name, content)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
