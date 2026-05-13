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
		// Multi-step (>2) repeats use the block form.
		if s.Repeat != nil && len(s.Repeat.Steps) > 2 {
			r := s.Repeat
			if r.Label != "" {
				fmt.Fprintf(&b, "%s %dx", r.Label, r.Reps)
			} else {
				fmt.Fprintf(&b, "%dx", r.Reps)
			}
			if s.Note != "" {
				b.WriteString(" -- ")
				b.WriteString(s.Note)
			}
			b.WriteString("\n")
			for _, sub := range r.Steps {
				b.WriteString("- ")
				b.WriteString(renderSimple(sub))
				if sub.Note != "" {
					b.WriteString(" -- ")
					b.WriteString(sub.Note)
				}
				b.WriteString("\n")
			}
			continue
		}
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
		// Inline form is only used for exactly two sub-steps. Multi-step
		// repeats are rendered separately by RenderDSL using the block
		// form, so this branch shouldn't be reached for them; fall back
		// to the inline header for safety.
		if len(r.Steps) == 2 {
			work := renderSimple(r.Steps[0])
			if r.Steps[0].Note != "" {
				work += " -- " + r.Steps[0].Note
			}
			rest := renderSimple(r.Steps[1])
			if r.Steps[1].Note != "" {
				rest += " -- " + r.Steps[1].Note
			}
			return fmt.Sprintf("%dx (%s / %s)", r.Reps, work, rest)
		}
		return fmt.Sprintf("%dx", r.Reps)
	case s.Ramp != nil:
		return renderRampDSL(*s.Ramp)
	default:
		return ""
	}
}

func renderRampDSL(r RampStep) string {
	switch r.RampType {
	case "percent":
		return fmt.Sprintf("%s ramp %d%%-%d%%", r.Duration.Raw, r.FromPercent, r.ToPercent)
	case "pace_percent":
		return fmt.Sprintf("%s ramp %d%%-%d%% Pace", r.Duration.Raw, r.FromPercent, r.ToPercent)
	default: // "zone" or legacy
		return fmt.Sprintf("%s ramp %s-%s", r.Duration.Raw, r.From, r.To)
	}
}

func renderSimple(s SimpleStep) string {
	parts := []string{}
	if s.Label != "" {
		parts = append(parts, s.Label)
	}
	parts = append(parts, s.Amount.String())
	parts = append(parts, s.Intensity.String())
	if s.Cadence != nil {
		if s.Cadence.RPMTo != 0 {
			parts = append(parts, fmt.Sprintf("%d-%drpm", s.Cadence.RPM, s.Cadence.RPMTo))
		} else {
			parts = append(parts, fmt.Sprintf("%drpm", s.Cadence.RPM))
		}
	}
	return strings.Join(parts, " ")
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
			if r.Label != "" {
				fmt.Fprintf(&b, "%s %dx\n", r.Label, r.Reps)
			} else {
				fmt.Fprintf(&b, "%dx\n", r.Reps)
			}
			for i, sub := range r.Steps {
				if i > 0 {
					b.WriteString("\n")
				}
				writeICUSimple(&b, sub, sub.Note)
			}
			if s.Note != "" {
				b.WriteString("\n# ")
				b.WriteString(s.Note)
			}
		case s.Ramp != nil:
			writeICURamp(&b, *s.Ramp, s.Note)
		}
		b.WriteString("\n")
		prevWasRepeat = isRepeat
	}
	return b.String()
}

func writeICUSimple(b *strings.Builder, s SimpleStep, note string) {
	b.WriteString("- ")
	if s.Label != "" {
		b.WriteString(s.Label)
		b.WriteString(" ")
	}
	b.WriteString(icuAmount(s.Amount))
	b.WriteString(" ")
	b.WriteString(icuIntensity(s.Intensity))
	if s.Cadence != nil {
		if s.Cadence.RPMTo != 0 {
			fmt.Fprintf(b, " %d-%drpm", s.Cadence.RPM, s.Cadence.RPMTo)
		} else {
			fmt.Fprintf(b, " %drpm", s.Cadence.RPM)
		}
	}
	if note != "" {
		b.WriteString(" ")
		b.WriteString(note)
	}
}

func writeICURamp(b *strings.Builder, r RampStep, note string) {
	b.WriteString("- ")
	b.WriteString(r.Duration.Raw)
	b.WriteString(" ramp ")
	switch r.RampType {
	case "percent":
		fmt.Fprintf(b, "%d%%-%d%%", r.FromPercent, r.ToPercent)
	case "pace_percent":
		fmt.Fprintf(b, "%d%%-%d%% Pace", r.FromPercent, r.ToPercent)
	default: // "zone"
		fmt.Fprintf(b, "%s-%s", r.From, r.To)
	}
	if note != "" {
		b.WriteString(" ")
		b.WriteString(note)
	}
}

// icuIntensity renders an Intensity for the intervals.icu description string.
// For legacy /km pace targets, strips the /km suffix to use plain "M:SS" or
// adds the "Pace" keyword for IsPaceWord targets.
func icuIntensity(i Intensity) string {
	if i.Pace != nil {
		p := i.Pace
		if p.IsPaceWord {
			return p.Raw // already has "Pace" suffix
		}
		// Legacy /km or /mi — keep as-is
		return p.Raw
	}
	return i.String()
}

// icuAmount renders an Amount for the intervals.icu description string.
// Distances in metres must use the "mtr" suffix — intervals.icu treats
// bare "m" as minutes, not metres.
func icuAmount(a Amount) string {
	if a.Distance != nil && a.Distance.Unit == "m" {
		return fmt.Sprintf("%dmtr", a.Distance.Value)
	}
	return a.String()
}
