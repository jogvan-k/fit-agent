package render

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jogvan-k/fit-agent/internal/icu"
)

// Sentinel markers delimiting the machine-owned YAML block inside a
// `planned-workouts/YYYY-MM-DD.md` file. Everything between (and
// including) these two lines is rewritten on every render; the agent
// must not edit between them.
const (
	ICUBlockBegin = "<!-- fit-agent:icu:begin -->"
	ICUBlockEnd   = "<!-- fit-agent:icu:end -->"
)

// ICUBlockInput is the data ICUBlock needs to render a single day's
// machine block.
type ICUBlockInput struct {
	// Date is the local calendar date the block covers; written into
	// a comment so a human skimming the file knows which day the
	// block represents.
	Date time.Time
	// GeneratedAt timestamps the block; omitted when zero so callers
	// that want deterministic output can leave it empty.
	GeneratedAt time.Time
	// Events is the list of icu events that fall on Date. Ordering
	// is normalised by start_date_local then id so re-rendering the
	// same data produces byte-identical output.
	Events []icu.Event
}

// ICUBlock renders the contents of the machine-owned region of a
// planned-workout-day markdown file, including the opening and closing
// HTML-comment sentinels. The output ends with a newline.
//
// Format:
//
//	<!-- fit-agent:icu:begin -->
//	```yaml
//	# Machine-managed: rewritten on every `fit-agent render planned`.
//	# Do not edit between the begin/end sentinels.
//	generated_at: 2026-05-03T20:14:00Z
//	source: intervals.icu
//	events:
//	  - icu_event_id: 12345
//	    name: "Z2 Endurance"
//	    type: Ride
//	    category: WORKOUT
//	    moving_time_s: 4500
//	    start_date_local: 2026-05-04T07:00:00
//	    description: |
//	      - 10m Z1
//	      - 60m Z2
//	      - 5m Z1
//	```
//	<!-- fit-agent:icu:end -->
func ICUBlock(in ICUBlockInput) []byte {
	evs := append([]icu.Event(nil), in.Events...)
	sort.SliceStable(evs, func(i, j int) bool {
		if evs[i].StartDateLocal != evs[j].StartDateLocal {
			return evs[i].StartDateLocal < evs[j].StartDateLocal
		}
		return evs[i].ID < evs[j].ID
	})

	var b bytes.Buffer
	b.WriteString(ICUBlockBegin)
	b.WriteByte('\n')
	b.WriteString("```yaml\n")
	b.WriteString("# Machine-managed: rewritten on every `fit-agent render planned`.\n")
	b.WriteString("# Do not edit between the begin/end sentinels.\n")
	if !in.GeneratedAt.IsZero() {
		fmt.Fprintf(&b, "generated_at: %s\n", in.GeneratedAt.UTC().Format(time.RFC3339))
	}
	b.WriteString("source: intervals.icu\n")
	if len(evs) == 0 {
		b.WriteString("events: []\n")
	} else {
		b.WriteString("events:\n")
		for _, ev := range evs {
			writeICUEvent(&b, ev)
		}
	}
	b.WriteString("```\n")
	b.WriteString(ICUBlockEnd)
	b.WriteByte('\n')
	return b.Bytes()
}

func writeICUEvent(b *bytes.Buffer, ev icu.Event) {
	fmt.Fprintf(b, "  - icu_event_id: %d\n", ev.ID)
	if ev.Name != "" {
		fmt.Fprintf(b, "    name: %s\n", yamlString(ev.Name))
	}
	if ev.Type != "" {
		fmt.Fprintf(b, "    type: %s\n", yamlString(ev.Type))
	}
	if ev.Category != "" {
		fmt.Fprintf(b, "    category: %s\n", yamlString(ev.Category))
	}
	if ev.MovingTime > 0 {
		fmt.Fprintf(b, "    moving_time_s: %d\n", ev.MovingTime)
	}
	if ev.StartDateLocal != "" {
		fmt.Fprintf(b, "    start_date_local: %s\n", yamlString(ev.StartDateLocal))
	}
	if ev.Indoor != nil {
		fmt.Fprintf(b, "    indoor: %t\n", *ev.Indoor)
	}
	if ev.PairedActivityID != "" {
		fmt.Fprintf(b, "    completed: true\n")
		fmt.Fprintf(b, "    icu_activity_id: %s\n", ev.PairedActivityID)
	}
	if desc := strings.TrimRight(ev.Description, "\n"); desc != "" {
		b.WriteString("    description: ")
		b.WriteString(yamlBlockScalar(desc, 4))
		b.WriteByte('\n')
	}
}

// EmptyPlannedDay returns the agent-owned skeleton for a new
// planned-workouts/<date>.md file. The returned bytes contain the
// frontmatter with an empty workouts: list, a blank line, the machine
// block, and a trailing newline. Callers typically follow up with
// [SpliceICUBlock] when they have an icu block to insert in place of
// the placeholder one rendered here.
func EmptyPlannedDay(date time.Time) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	b.WriteString("fit-agent:\n")
	b.WriteString("  kind: planned-workout-day\n")
	fmt.Fprintf(&b, "  date: %s\n", date.Format("2006-01-02"))
	b.WriteString("workouts: []\n")
	b.WriteString("---\n\n")
	b.Write(ICUBlock(ICUBlockInput{Date: date}))
	return b.Bytes()
}
