package render

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jogvan-k/fit-agent/internal/icu"
)

// PlannedWorkoutDay is the input to [PlannedWorkoutDayMarkdown].
//
// The output is a single markdown file per local calendar date. Each
// workout becomes one frontmatter row plus one body section.
type PlannedWorkoutDay struct {
	Date     time.Time
	Workouts []PlannedWorkout
}

// PlannedWorkout pairs an intervals.icu Event with the structured
// fit-workout DSL body the agent authored.
//
// The DSL body is opaque to this package: render emits it inside a
// ```fit-workout fenced block verbatim. The workoutdsl package owns
// validation and conversion to the icu workout-description string.
type PlannedWorkout struct {
	// Event carries the icu-side metadata. The Event.ID may be 0
	// (frontmatter renders icu_event_id: null) until push-workouts
	// has POSTed the workout for the first time.
	Event icu.Event
	// DSLBody is the contents of the ```fit-workout``` fence, sans
	// the fence markers. A trailing newline is normalised.
	DSLBody string
	// Notes is optional prose that appears between the heading and
	// the DSL fence. Markdown is allowed.
	Notes string
}

// PlannedWorkoutDayMarkdown renders one day's planned workouts.
//
// The output begins with YAML frontmatter (a small fit-agent
// metadata block plus a per-workout summary list) followed by one
// `## <name>` section per workout. Each section ends with a
// ```fit-workout``` fenced block carrying the DSL.
func PlannedWorkoutDayMarkdown(day PlannedWorkoutDay) ([]byte, error) {
	if day.Date.IsZero() {
		return nil, fmt.Errorf("PlannedWorkoutDay.Date is required")
	}
	wks := append([]PlannedWorkout(nil), day.Workouts...)
	sort.SliceStable(wks, func(i, j int) bool {
		return wks[i].Event.StartDateLocal < wks[j].Event.StartDateLocal
	})

	var b bytes.Buffer
	b.WriteString("---\n")
	b.WriteString("fit-agent:\n")
	b.WriteString("  kind: planned-workout-day\n")
	fmt.Fprintf(&b, "  date: %s\n", day.Date.Format("2006-01-02"))
	b.WriteString("workouts:\n")
	for _, w := range wks {
		writePlannedFrontmatterEntry(&b, w)
	}
	b.WriteString("---\n\n")

	for i, w := range wks {
		if i > 0 {
			b.WriteString("\n")
		}
		writePlannedSection(&b, w)
	}
	return b.Bytes(), nil
}

func writePlannedFrontmatterEntry(b *bytes.Buffer, w PlannedWorkout) {
	fmt.Fprintf(b, "  - name: %s\n", yamlString(w.Event.Name))
	if w.Event.Type != "" {
		fmt.Fprintf(b, "    type: %s\n", yamlString(w.Event.Type))
	}
	if w.Event.MovingTime > 0 {
		fmt.Fprintf(b, "    moving_time_s: %d\n", w.Event.MovingTime)
	}
	if w.Event.ID != 0 {
		fmt.Fprintf(b, "    icu_event_id: %d\n", w.Event.ID)
	} else {
		b.WriteString("    icu_event_id: null\n")
	}
}

func writePlannedSection(b *bytes.Buffer, w PlannedWorkout) {
	heading := w.Event.Name
	if heading == "" {
		heading = "Planned workout"
	}
	fmt.Fprintf(b, "## %s\n\n", heading)
	if notes := strings.TrimSpace(w.Notes); notes != "" {
		b.WriteString(notes)
		b.WriteString("\n\n")
	}
	b.WriteString("```fit-workout\n")
	body := strings.TrimRight(w.DSLBody, "\n")
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	b.WriteString("```\n")
}
