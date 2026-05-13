package workoutdsl

import "encoding/json"

// WorkoutDocStep is one step in the intervals.icu workout_doc JSON
// structure. All fields are omitempty so only the relevant ones are
// serialised.
type WorkoutDocStep struct {
	// Distance in metres (set when the DSL step uses a distance amount).
	Distance int `json:"distance,omitempty"`
	// Duration in seconds (set when the DSL step uses a duration amount,
	// or as an estimate when only distance is known).
	Duration int `json:"duration,omitempty"`
	// Pace target (set when the DSL intensity is a Pace).
	Pace *WorkoutDocPace `json:"pace,omitempty"`
	// Power target (set when the DSL intensity is a Zone or Percent).
	Power *WorkoutDocPower `json:"power,omitempty"`
	// HR target (set when the DSL intensity is an HR target).
	HR *WorkoutDocHR `json:"hr,omitempty"`
	// Intensity string (e.g. "recovery").
	Intensity string `json:"intensity,omitempty"`
	// Text label for the step.
	Text string `json:"text,omitempty"`
	// Reps and nested steps for repeat blocks.
	Reps  int               `json:"reps,omitempty"`
	Steps []*WorkoutDocStep `json:"steps,omitempty"`
}

// WorkoutDocPace is the pace target sub-object accepted by
// intervals.icu. units="secs_km" means value is seconds-per-km.
type WorkoutDocPace struct {
	Units string  `json:"units"`
	Value float64 `json:"value,omitempty"`
	Start float64 `json:"start,omitempty"`
	End   float64 `json:"end,omitempty"`
}

// WorkoutDocPower is the power/zone target sub-object.
type WorkoutDocPower struct {
	Units string `json:"units"`
	Value int    `json:"value,omitempty"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
}

// WorkoutDocHR is the HR target sub-object.
type WorkoutDocHR struct {
	Units string `json:"units"`
	Value int    `json:"value,omitempty"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
}

// WorkoutDoc is the top-level workout_doc JSON object sent to
// intervals.icu alongside the description text.
type WorkoutDoc struct {
	Steps    []*WorkoutDocStep `json:"steps"`
	Distance int               `json:"distance,omitempty"`
	Duration int               `json:"duration,omitempty"`
}

// HasTargets returns true if any step in the workout has a structured
// intensity target (pace, zone, or percent). Used by pushorch to decide
// whether to include a workout_doc in the ICU event payload.
func HasTargets(w *Workout) bool {
	if w == nil {
		return false
	}
	for _, s := range w.Steps {
		if stepHasTarget(s) {
			return true
		}
	}
	return false
}

func stepHasTarget(s Step) bool {
	switch {
	case s.Simple != nil:
		return intensityHasTarget(s.Simple.Intensity)
	case s.Repeat != nil:
		for _, sub := range s.Repeat.Steps {
			if intensityHasTarget(sub.Intensity) {
				return true
			}
		}
	case s.Ramp != nil:
		return true
	}
	return false
}

func intensityHasTarget(i Intensity) bool {
	return i.Zone != nil || i.Percent != nil || i.Pace != nil ||
		i.HR != nil || i.PaceZone != nil || i.PacePercent != nil ||
		i.Watts != nil || i.CustomZone != nil || i.IsRecovery
}

// RenderWorkoutDoc converts a parsed Workout into the workout_doc JSON
// structure that intervals.icu uses for structured workout syncing to
// Garmin and other devices.
//
// Pace intensities are emitted as pace targets (units="secs_km").
// Zone and percent intensities are emitted as power targets so existing
// behaviour is preserved. Named intensities (easy, recovery, etc.) and
// ramps produce steps without a target.
func RenderWorkoutDoc(w *Workout) (json.RawMessage, error) {
	if w == nil {
		return json.Marshal(WorkoutDoc{Steps: []*WorkoutDocStep{}})
	}
	doc := &WorkoutDoc{}
	for _, s := range w.Steps {
		step := buildDocStep(s)
		doc.Steps = append(doc.Steps, step)
		doc.Distance += step.Distance
		doc.Duration += step.Duration
	}
	return json.Marshal(doc)
}

func buildDocStep(s Step) *WorkoutDocStep {
	switch {
	case s.Simple != nil:
		return simpleDocStep(*s.Simple)
	case s.Repeat != nil:
		return repeatDocStep(*s.Repeat)
	case s.Ramp != nil:
		return rampDocStep(*s.Ramp)
	}
	return &WorkoutDocStep{}
}

func simpleDocStep(s SimpleStep) *WorkoutDocStep {
	step := &WorkoutDocStep{}
	// Label → text
	if s.Label != "" {
		step.Text = s.Label
	}
	// Amount.
	if s.Amount.Duration != nil {
		step.Duration = s.Amount.Duration.Seconds
	} else if s.Amount.Distance != nil {
		step.Distance = distanceMetres(s.Amount.Distance)
		// Provide an estimated duration so ICU can render a timeline.
		// We leave it zero here; ICU fills it in after accepting the doc.
	}
	// Intensity.
	applyIntensityToDocStep(step, s.Intensity, s.Amount)
	return step
}

func applyIntensityToDocStep(step *WorkoutDocStep, i Intensity, amt Amount) {
	switch {
	case i.Pace != nil:
		p := i.Pace
		if p.SecondsEnd != 0 {
			// Range pace: SecondsEnd is the slow end, Seconds is the fast end
			step.Pace = &WorkoutDocPace{
				Units: "secs",
				Start: float64(p.SecondsEnd),
				End:   float64(p.Seconds),
			}
		} else {
			step.Pace = &WorkoutDocPace{
				Units: "secs_km",
				Value: float64(p.Seconds),
			}
		}
		// Also populate Duration as an estimate so ICU can compute
		// a timeline even before it knows the athlete's pace.
		if step.Distance > 0 && step.Duration == 0 && p.Seconds > 0 {
			step.Duration = step.Distance * p.Seconds / 1000
		}
	case i.PaceZone != nil:
		pz := i.PaceZone
		if pz.ZoneTo != 0 {
			step.Pace = &WorkoutDocPace{
				Units: "pace_zone",
				Start: float64(pz.Zone),
				End:   float64(pz.ZoneTo),
			}
		} else {
			step.Pace = &WorkoutDocPace{
				Units: "pace_zone",
				Value: float64(pz.Zone),
			}
		}
	case i.PacePercent != nil:
		pp := i.PacePercent
		if pp.PercentTo != 0 {
			step.Pace = &WorkoutDocPace{
				Units: "percent",
				Start: float64(pp.Percent),
				End:   float64(pp.PercentTo),
			}
		} else {
			step.Pace = &WorkoutDocPace{
				Units: "percent",
				Value: float64(pp.Percent),
			}
		}
	case i.Zone != nil:
		step.Power = &WorkoutDocPower{Units: "power_zone", Value: i.Zone.N}
	case i.Percent != nil:
		step.Power = &WorkoutDocPower{Units: "percent_ftp", Value: *i.Percent}
	case i.Watts != nil:
		w := i.Watts
		if w.WattsTo != 0 {
			step.Power = &WorkoutDocPower{Units: "watts", Start: w.Watts, End: w.WattsTo}
		} else {
			step.Power = &WorkoutDocPower{Units: "watts", Value: w.Watts}
		}
	case i.CustomZone != nil:
		cz := i.CustomZone
		if cz.ZoneTo != 0 {
			step.Power = &WorkoutDocPower{Units: "custom_zone", Start: cz.Zone, End: cz.ZoneTo}
		} else {
			step.Power = &WorkoutDocPower{Units: "custom_zone", Value: cz.Zone}
		}
	case i.HR != nil:
		hr := i.HR
		if hr.Zone != nil {
			if hr.ZoneTo != nil {
				step.HR = &WorkoutDocHR{Units: "zone", Start: hr.Zone.N, End: hr.ZoneTo.N}
			} else {
				step.HR = &WorkoutDocHR{Units: "zone", Value: hr.Zone.N}
			}
		} else {
			units := "percent_max"
			if hr.IsLTHR {
				units = "percent_lthr"
			}
			if hr.PercentTo != 0 {
				step.HR = &WorkoutDocHR{Units: units, Start: hr.Percent, End: hr.PercentTo}
			} else {
				step.HR = &WorkoutDocHR{Units: units, Value: hr.Percent}
			}
		}
	case i.IsRecovery:
		step.Intensity = "recovery"
		// Named intensities (easy, threshold, etc.) and nil →
		// no target field; device shows "no target" which is correct for
		// easy/recovery steps and acceptable for named-zone steps until a
		// richer mapping is added.
	}
}

func repeatDocStep(r RepeatStep) *WorkoutDocStep {
	step := &WorkoutDocStep{
		Reps: r.Reps,
	}
	if r.Label != "" {
		step.Text = r.Label
	}
	var totalDist, totalDur int
	for _, sub := range r.Steps {
		child := simpleDocStep(sub)
		step.Steps = append(step.Steps, child)
		totalDist += child.Distance
		totalDur += child.Duration
	}
	// Roll up totals scaled by reps.
	step.Distance = totalDist * r.Reps
	step.Duration = totalDur * r.Reps
	return step
}

func rampDocStep(r RampStep) *WorkoutDocStep {
	// Ramps have no single pace/power target; emit duration only.
	return &WorkoutDocStep{Duration: r.Duration.Seconds}
}

// distanceMetres converts a Distance to metres.
func distanceMetres(d *Distance) int {
	switch d.Unit {
	case "km":
		return d.Value * 1000
	case "km_frac":
		return d.Value // already metres
	case "y":
		// 1 yard ≈ 0.9144 m; round to nearest metre.
		return int(float64(d.Value) * 0.9144)
	default: // "m"
		return d.Value
	}
}
