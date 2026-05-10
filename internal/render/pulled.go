package render

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/jogvan-k/fit-agent/internal/icu"
)

// PulledWorkout is one intervals.icu-authored planned workout that the
// agent did not create locally. It is materialised by sync-workouts as
// a read-only `.icu.md` file under planned-workouts/, so the agent can
// see what is on the calendar without confusing it with files the
// workout-builder has authored.
type PulledWorkout struct {
	// Event is the icu-side record. Event.ID and Event.Description
	// are required; everything else is best-effort.
	Event icu.Event
	// Date is the local calendar date the workout falls on. The
	// caller is responsible for translating Event.StartDateLocal
	// into the athlete-local TZ.
	Date time.Time
	// GeneratedAt timestamps the rendered file's frontmatter so the
	// agent can tell when sync-workouts last refreshed it. When zero
	// the field is omitted.
	GeneratedAt time.Time
}

// PulledWorkoutDayMarkdown renders a single icu-authored workout into a
// read-only markdown file.
//
// Output shape:
//
//	---
//	fit-agent:
//	  kind: pulled-workout-day
//	  date: 2026-05-04
//	  read_only: true
//	  source: intervals.icu
//	  generated_at: 2026-05-03T20:14:00Z
//	workout:
//	  icu_event_id: 12345
//	  name: "Z2 Endurance"
//	  type: Ride
//	  moving_time_s: 4500
//	---
//
//	# Z2 Endurance
//
//	> Read-only: this file mirrors a planned workout authored on
//	> intervals.icu. Edit it on intervals.icu (or convert it to a
//	> locally-authored `.md` file) — `sync-workouts` overwrites it on
//	> every run.
//
//	## ICU description
//
//	```
//	- 10m Z1
//	- 60m Z2
//	- 5m Z1
//	```
//
// The "ICU description" block is a verbatim copy of
// [icu.Event.Description]; we deliberately do not attempt to reverse it
// back into the `fit-workout` DSL since the conversion is lossy.
func PulledWorkoutDayMarkdown(p PulledWorkout) ([]byte, error) {
	if p.Event.ID == 0 {
		return nil, fmt.Errorf("PulledWorkout.Event.ID is required")
	}
	if p.Date.IsZero() {
		return nil, fmt.Errorf("PulledWorkout.Date is required")
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.WriteString("fit-agent:\n")
	b.WriteString("  kind: pulled-workout-day\n")
	fmt.Fprintf(&b, "  date: %s\n", p.Date.Format("2006-01-02"))
	b.WriteString("  read_only: true\n")
	b.WriteString("  source: intervals.icu\n")
	if !p.GeneratedAt.IsZero() {
		fmt.Fprintf(&b, "  generated_at: %s\n", p.GeneratedAt.UTC().Format(time.RFC3339))
	}
	b.WriteString("workout:\n")
	fmt.Fprintf(&b, "  icu_event_id: %d\n", p.Event.ID)
	if p.Event.Name != "" {
		fmt.Fprintf(&b, "  name: %s\n", yamlString(p.Event.Name))
	}
	if p.Event.Type != "" {
		fmt.Fprintf(&b, "  type: %s\n", yamlString(p.Event.Type))
	}
	if p.Event.MovingTime > 0 {
		fmt.Fprintf(&b, "  moving_time_s: %d\n", p.Event.MovingTime)
	}
	if p.Event.Category != "" {
		fmt.Fprintf(&b, "  category: %s\n", yamlString(p.Event.Category))
	}
	b.WriteString("---\n\n")

	heading := strings.TrimSpace(p.Event.Name)
	if heading == "" {
		heading = "Planned workout"
	}
	fmt.Fprintf(&b, "# %s\n\n", heading)
	b.WriteString("> Read-only: this file mirrors a planned workout authored on\n")
	b.WriteString("> intervals.icu. Edit it on intervals.icu (or convert it to a\n")
	b.WriteString("> locally-authored `.md` file) — `sync-workouts` overwrites it on\n")
	b.WriteString("> every run.\n\n")

	b.WriteString("## ICU description\n\n")
	desc := strings.TrimRight(p.Event.Description, "\n")
	b.WriteString("```\n")
	if desc != "" {
		b.WriteString(desc)
		b.WriteString("\n")
	}
	b.WriteString("```\n")
	return b.Bytes(), nil
}
