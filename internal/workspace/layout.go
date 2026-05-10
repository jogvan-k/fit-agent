// Package workspace owns all paths and on-disk conventions for a
// fit-agent workspace.
//
// A workspace is a directory containing a mix of agent-owned narrative
// files (markdown) and machine-owned data files (YAML), plus a
// .cache/ tree holding raw intervals.icu JSON and FIT payloads.
//
// This package is the single source of truth for those paths; callers
// must never assemble workspace-relative paths by hand. It also provides
// the atomic write helper every writer in the project uses, and the
// ownership guard that prevents machine-driven code from clobbering
// agent-owned files.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Layout resolves all canonical paths under a workspace root.
//
// The zero value is not usable; construct via [New].
type Layout struct {
	// Root is the absolute path to the workspace directory.
	Root string
}

// New returns a Layout rooted at the given workspace path.
//
// The path is cleaned but not required to exist; callers that mutate
// the workspace should use [Layout.EnsureDirs] first.
func New(root string) Layout {
	return Layout{Root: filepath.Clean(root)}
}

// Path joins parts onto the workspace root.
func (l Layout) Path(parts ...string) string {
	return filepath.Join(append([]string{l.Root}, parts...)...)
}

// FitAgentDir is the machine-owned subtree (data + cache).
func (l Layout) FitAgentDir() string { return l.Path("fit-agent") }

// ActivitiesDir holds per-day activity YAML files.
func (l Layout) ActivitiesDir() string { return l.Path("fit-agent", "activities") }

// WellnessDir holds per-month wellness YAML files.
func (l Layout) WellnessDir() string { return l.Path("fit-agent", "wellness") }

// PlannedWorkoutsDir holds per-day planned-workout markdown files.
func (l Layout) PlannedWorkoutsDir() string { return l.Path("fit-agent", "planned-workouts") }

// CacheDir is the root of the .cache/ subtree.
func (l Layout) CacheDir() string { return l.Path("fit-agent", ".cache") }

// CacheActivitiesDir holds raw intervals.icu activity JSON + FIT files.
func (l Layout) CacheActivitiesDir() string { return l.Path("fit-agent", ".cache", "activities") }

// CacheWellnessDir holds raw monthly wellness JSON.
func (l Layout) CacheWellnessDir() string { return l.Path("fit-agent", ".cache", "wellness") }

// CacheEventsDir holds raw planned-workout JSON.
func (l Layout) CacheEventsDir() string { return l.Path("fit-agent", ".cache", "events") }

// CacheAthletePath is the cached athlete profile JSON.
func (l Layout) CacheAthletePath() string { return l.Path("fit-agent", ".cache", "athlete.json") }

// AthleteProfilePath is the agent-owned athlete profile markdown.
func (l Layout) AthleteProfilePath() string { return l.Path("ATHLETE-PROFILE.md") }

// TrainingPlanPath is the agent-owned training plan markdown.
func (l Layout) TrainingPlanPath() string { return l.Path("TRAINING-PLAN.md") }

// ReadmePath is the agent-owned workspace README.
func (l Layout) ReadmePath() string { return l.Path("README.md") }

// SkillsDir holds per-skill subdirectories.
func (l Layout) SkillsDir() string { return l.Path("skills") }

// PointerPath is the workspace .fit-agent.toml pointer file.
func (l Layout) PointerPath() string { return l.Path(".fit-agent.toml") }

// ActivityDayPath returns the YAML path for the given local date.
func (l Layout) ActivityDayPath(date time.Time) string {
	return filepath.Join(l.ActivitiesDir(), date.Format("2006-01-02")+".yaml")
}

// WellnessMonthPath returns the YAML path for the given month.
//
// Only the year and month components are used.
func (l Layout) WellnessMonthPath(month time.Time) string {
	return filepath.Join(l.WellnessDir(), month.Format("2006-01")+".yaml")
}

// PlannedWorkoutDayPath returns the markdown path for a planned-workout
// day. This file is shared-ownership: the agent authors it and
// sync-workouts stamps icu_event_id back into it after a successful push.
func (l Layout) PlannedWorkoutDayPath(date time.Time) string {
	return filepath.Join(l.PlannedWorkoutsDir(), date.Format("2006-01-02")+".md")
}

// PulledWorkoutDayPath returns the markdown path used to materialise an
// intervals.icu-authored workout that the agent did not author locally.
//
// These files share the planned-workouts/ directory but use the
// `.icu.md` suffix so they are visually and programmatically
// distinguishable from agent-authored `.md` files. They are
// machine-owned: regenerated on every `fit-agent sync-workouts` and
// never edited by the agent.
func (l Layout) PulledWorkoutDayPath(date time.Time, icuID string) string {
	return filepath.Join(l.PlannedWorkoutsDir(), date.Format("2006-01-02")+"."+icuID+".icu.md")
}

// CacheActivityJSONPath returns the cache path for an activity's raw
// intervals.icu JSON payload.
func (l Layout) CacheActivityJSONPath(icuID string) string {
	return filepath.Join(l.CacheActivitiesDir(), icuID+".json")
}

// CacheActivityFITPath returns the cache path for an activity's raw
// FIT payload.
func (l Layout) CacheActivityFITPath(icuID string) string {
	return filepath.Join(l.CacheActivitiesDir(), icuID+".fit")
}

// CacheWellnessMonthPath returns the cache path for a month's raw
// wellness JSON payload.
func (l Layout) CacheWellnessMonthPath(month time.Time) string {
	return filepath.Join(l.CacheWellnessDir(), month.Format("2006-01")+".json")
}

// CacheEventPath returns the cache path for a planned-workout event JSON.
func (l Layout) CacheEventPath(icuID string) string {
	return filepath.Join(l.CacheEventsDir(), icuID+".json")
}

// MachineDirs are the machine-owned directories that fetch may create.
func (l Layout) MachineDirs() []string {
	return []string{
		l.ActivitiesDir(),
		l.WellnessDir(),
		l.PlannedWorkoutsDir(),
		l.CacheActivitiesDir(),
		l.CacheWellnessDir(),
		l.CacheEventsDir(),
	}
}

// EnsureDirs creates every machine-owned directory under the workspace
// with mode 0755. Pre-existing directories are left untouched.
func (l Layout) EnsureDirs() error {
	for _, d := range l.MachineDirs() {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}
