package workoutdsl

import (
	"fmt"
	"strings"
)

// RenderDSL emits the canonical DSL form of the workout. Parsing the
// output and rendering again must be a fixed point (round-trip).
func RenderDSL(w *Workout) string {
	if w == nil {
		return ""
	}
	var b strings.Builder
	for _, s := range w.Steps {
		b.WriteString("- ")
		b.WriteString(renderStepBody(s))
		if s.Note != "" {
			b.WriteString(" -- ")
			b.WriteString(s.Note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderStepBody(s Step) string {
	switch {
	case s.Simple != nil:
		return renderSimple(*s.Simple)
	case s.Repeat != nil:
		r := s.Repeat
		work := renderSimple(r.Work)
		if r.Work.Note != "" {
			work += " -- " + r.Work.Note
		}
		rest := renderSimple(r.Rest)
		if r.Rest.Note != "" {
			rest += " -- " + r.Rest.Note
		}
		return fmt.Sprintf("%dx (%s / %s)", r.Reps, work, rest)
	case s.Ramp != nil:
		return fmt.Sprintf("%s ramp %s-%s", s.Ramp.Duration.Raw, s.Ramp.From, s.Ramp.To)
	default:
		return ""
	}
}

func renderSimple(s SimpleStep) string {
	return fmt.Sprintf("%s %s", s.Amount, s.Intensity)
}

// RenderICU emits the intervals.icu workout-description string for the
// workout. Format follows the cookbook examples: each simple step on
// its own line prefixed with "- "; repeats expanded as "Nx" header
// followed by the work and rest steps; ramps emitted as
// "<duration> ramp <fromZone>-<toZone>". Notes are appended after a
// space-separated "--" suffix (intervals.icu treats step text after
// the duration/intensity as a note).
//
// See: https://forum.intervals.icu/t/uploading-planned-workouts-to-intervals-icu/63624
// and the cookbook example with "- 15m 55% Warmup\n\n3x\n- 1m 150%\n- 1m 50%".
func RenderICU(w *Workout) string {
	if w == nil {
		return ""
	}
	var b strings.Builder
	prevWasRepeat := false
	for i, s := range w.Steps {
		isRepeat := s.Repeat != nil
		// Blank-line separator before a repeat block, or after one.
		if i > 0 && (isRepeat || prevWasRepeat) {
			b.WriteString("\n")
		}
		switch {
		case s.Simple != nil:
			writeICUSimple(&b, *s.Simple, s.Note)
		case s.Repeat != nil:
			r := s.Repeat
			fmt.Fprintf(&b, "%dx\n", r.Reps)
			writeICUSimple(&b, r.Work, r.Work.Note)
			b.WriteString("\n")
			writeICUSimple(&b, r.Rest, r.Rest.Note)
			if s.Note != "" {
				b.WriteString("\n# ")
				b.WriteString(s.Note)
			}
		case s.Ramp != nil:
			fmt.Fprintf(&b, "- %s ramp %s-%s", s.Ramp.Duration.Raw, s.Ramp.From, s.Ramp.To)
			if s.Note != "" {
				b.WriteString(" ")
				b.WriteString(s.Note)
			}
		}
		b.WriteString("\n")
		prevWasRepeat = isRepeat
	}
	return b.String()
}

func writeICUSimple(b *strings.Builder, s SimpleStep, note string) {
	fmt.Fprintf(b, "- %s %s", s.Amount, s.Intensity)
	if note != "" {
		b.WriteString(" ")
		b.WriteString(note)
	}
}
