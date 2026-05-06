package workoutdsl

import "fmt"

// Stats summarises a parsed workout for display.
type Stats struct {
	Steps          int
	TotalSeconds   int // duration-only; distance steps don't contribute
	HasDistance    bool
	HasRamp        bool
	NamedTargets   int
	PercentTargets int
	ZoneTargets    int
}

// Summary computes high-level stats for a parsed workout.
func Summary(w *Workout) Stats {
	if w == nil {
		return Stats{}
	}
	var st Stats
	st.Steps = len(w.Steps)
	for _, s := range w.Steps {
		switch {
		case s.Simple != nil:
			addSimple(&st, *s.Simple, 1)
		case s.Repeat != nil:
			addSimple(&st, s.Repeat.Work, s.Repeat.Reps)
			addSimple(&st, s.Repeat.Rest, s.Repeat.Reps)
		case s.Ramp != nil:
			st.HasRamp = true
			st.TotalSeconds += s.Ramp.Duration.Seconds
		}
	}
	return st
}

func addSimple(st *Stats, s SimpleStep, mult int) {
	if s.Amount.Duration != nil {
		st.TotalSeconds += s.Amount.Duration.Seconds * mult
	}
	if s.Amount.Distance != nil {
		st.HasDistance = true
	}
	switch {
	case s.Intensity.Zone != nil:
		st.ZoneTargets++
	case s.Intensity.Named != "":
		st.NamedTargets++
	case s.Intensity.Percent != nil:
		st.PercentTargets++
	}
}

// String renders stats as a one-line summary suitable for `lint`.
func (s Stats) String() string {
	mins := s.TotalSeconds / 60
	secs := s.TotalSeconds % 60
	flags := ""
	if s.HasDistance {
		flags += " +distance"
	}
	if s.HasRamp {
		flags += " +ramp"
	}
	return fmt.Sprintf("%d steps, ~%dm%02ds%s (zones=%d named=%d %%=%d)",
		s.Steps, mins, secs, flags, s.ZoneTargets, s.NamedTargets, s.PercentTargets)
}
