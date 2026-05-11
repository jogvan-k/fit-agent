// Package workoutdsl parses the fit-workout DSL used inside
// `fit-agent/planned-workouts/*.md` markdown files and converts it to
// the intervals.icu workout-description string accepted by the events
// API (see https://forum.intervals.icu/t/uploading-planned-workouts-to-intervals-icu/63624).
//
// Grammar (v1):
//
//	workout      := { step }
//	step         := simple | repeat | ramp
//	simple       := "- " amount SP intensity [SP "--" SP note]
//	repeat       := inlineRepeat | blockRepeat
//	inlineRepeat := "- " INT "x" SP "(" simple SP "/" SP simple ")" [SP "--" SP note]
//	blockRepeat  := INT "x" [SP "--" SP note] NL { simple NL }+
//	ramp         := "- " duration SP "ramp" SP zone "-" zone [SP "--" SP note]
//	amount       := duration | distance
//	duration     := { INT ("h"|"m"|"s") }+
//	distance     := INT ("m"|"km"|"y")
//	intensity    := zone | namedIntensity | percent
//	zone         := "Z" ("1".."6")
//	percent      := INT "%"
//	namedIntensity := "recovery"|"easy"|"tempo"|"threshold"|"vo2"|"anaerobic"|"sprint"
//
// Blank lines and lines beginning with "#" are ignored.
package workoutdsl

import "fmt"

// Workout is the parsed representation of a fit-workout block.
type Workout struct {
	Steps []Step
}

// Step is one element in the workout. Exactly one of Simple, Repeat or
// Ramp is non-nil.
type Step struct {
	Line   int // 1-indexed source line (within the DSL block)
	Simple *SimpleStep
	Repeat *RepeatStep
	Ramp   *RampStep
	Note   string // free-text note from "-- ..." (also held on the inner kind for repeat children)
}

// SimpleStep is a single chunk of work, e.g. "10m Z2".
type SimpleStep struct {
	Amount    Amount
	Intensity Intensity
	Note      string
}

// RepeatStep is a repeated group of two or more sub-steps. The legacy
// inline form `Nx (work / rest)` produces exactly two Steps; the
// block form `Nx\n- step\n- step\n...` produces N >= 2 Steps.
type RepeatStep struct {
	Reps  int
	Steps []SimpleStep
	Note  string
}

// RampStep is "20m ramp Z1-Z3".
type RampStep struct {
	Duration Duration
	From     Zone
	To       Zone
	Note     string
}

// Amount is either a Duration or a Distance. Exactly one field is set.
type Amount struct {
	Duration *Duration
	Distance *Distance
}

// Duration in seconds.
type Duration struct {
	Seconds int
	Raw     string // original token, e.g. "1h30m"
}

// Distance is a length with unit.
type Distance struct {
	Value int
	Unit  string // "m", "km", "y"
	Raw   string
}

// Intensity is a target intensity. Exactly one field is set.
type Intensity struct {
	Zone    *Zone
	Named   string // recovery|easy|tempo|threshold|vo2|anaerobic|sprint
	Percent *int   // FTP percent
}

// Zone is Z1..Z6.
type Zone struct {
	N int
}

// String renders the zone as "Z1".
func (z Zone) String() string { return fmt.Sprintf("Z%d", z.N) }

// String renders amount in canonical form.
func (a Amount) String() string {
	switch {
	case a.Duration != nil:
		return a.Duration.Raw
	case a.Distance != nil:
		return a.Distance.Raw
	default:
		return ""
	}
}

// String renders intensity in canonical form.
func (i Intensity) String() string {
	switch {
	case i.Zone != nil:
		return i.Zone.String()
	case i.Named != "":
		return i.Named
	case i.Percent != nil:
		return fmt.Sprintf("%d%%", *i.Percent)
	default:
		return ""
	}
}
