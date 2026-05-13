// Package workoutdsl parses the fit-workout DSL used inside
// `fit-agent/planned-workouts/*.md` markdown files and converts it to
// the intervals.icu workout-description string accepted by the events
// API (see https://forum.intervals.icu/t/uploading-planned-workouts-to-intervals-icu/63624).
//
// Grammar (v2):
//
// The grammar below is the machine-authoritative DSL spec.
// internal/templates/skills/workout-builder/SKILL.md is the
// human- and agent-facing spec and must be kept in sync with this
// grammar. Every intensity type, amount unit, and named keyword
// added here must also appear with an example in that skill file.
// See AGENTS.md § "Extending the workout DSL".
//
//	workout      := { step }
//	step         := simple | repeat | ramp
//	simple       := "- " [label SP] amount SP intensity [SP cadence] [SP "--" SP note]
//	repeat       := inlineRepeat | blockRepeat
//	inlineRepeat := "- " INT "x" SP "(" simple SP "/" SP simple ")" [SP "--" SP note]
//	blockRepeat  := [label SP] INT "x" [SP "--" SP note] NL { simple NL }+
//	ramp         := "- " duration SP "ramp" SP (zone "-" zone | INT "%" "-" INT "%" | INT "%" "-" INT "%" SP "Pace")
//	amount       := duration | distance
//	duration     := { INT ("h"|"m"|"s") }+
//	distance     := INT ("mtr"|"km"|"mi"|"y") | INT "m" (>=50)
//	intensity    := zone | namedIntensity | percent | pace | hrTarget | paceZone | pacePercent | watts | customZone | recovery
//	zone         := "Z" ("1".."6")
//	percent      := INT "%"
//	pace         := INT ":" INT ["-" INT ":" INT] [SP "Pace"]  -- absolute pace or range
//	paceZone     := "Z" INT ["-Z" INT] SP "Pace"
//	pacePercent  := INT "%" ["-" INT "%"] SP "Pace"
//	hrTarget     := (zone | INT "%") ["-" (zone | INT "%")] SP ("HR" | "LTHR")
//	watts        := INT "w" ["-" INT "w"? ]
//	customZone   := "CZ" INT ["-CZ"? INT]
//	recovery     := "intensity=recovery"
//	namedIntensity := "recovery"|"easy"|"tempo"|"threshold"|"vo2"|"anaerobic"|"sprint"
//	cadence      := INT "rpm" | INT "-" INT "rpm"
//
// Blank lines, lines beginning with "#", "---", "**", or "|" are ignored.
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
	Label     string        // optional text-to-athlete prefix e.g. "Run high"
	Amount    Amount
	Intensity Intensity
	Cadence   *CadenceRange // optional cadence target e.g. 90rpm
	Note      string
}

// RepeatStep is a repeated group of two or more sub-steps. The legacy
// inline form `Nx (work / rest)` produces exactly two Steps; the
// block form `Nx\n- step\n- step\n...` produces N >= 2 Steps.
type RepeatStep struct {
	Label string // optional label e.g. "Main Set" from "Main Set 5x"
	Reps  int
	Steps []SimpleStep
	Note  string
}

// RampStep is "20m ramp Z1-Z3" or "10m ramp 50%-75%" or "10m ramp 60-80% Pace".
type RampStep struct {
	Duration    Duration
	From        Zone
	To          Zone
	FromPercent int    // for percent ramps
	ToPercent   int    // for percent ramps
	RampType    string // "zone" | "percent" | "pace_percent"
	Note        string
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

// Intensity is a target intensity. Exactly one of the fields is set.
type Intensity struct {
	Zone         *Zone
	Named        string         // recovery|easy|tempo|threshold|vo2|anaerobic|sprint
	Percent      *int           // FTP percent
	Pace         *Pace          // run pace target e.g. 3:55/km or 3:55 Pace
	HR           *HRTarget      // Z2 HR, 70% HR, 95% LTHR, 70-80% HR, Z2-Z3 HR
	PaceZone     *PaceZoneTarget    // Z2 Pace, Z1-Z2 Pace
	PacePercent  *PacePercentTarget // 78-82% Pace
	Watts        *WattsTarget       // 220w, 200-240w
	CustomZone   *CustomZoneTarget  // CZ1, CZ2-CZ3
	IsRecovery   bool               // intensity=recovery
}

// HRTarget is a heart-rate target, e.g. Z2 HR, 70% HR, 95% LTHR, 70-80% HR.
type HRTarget struct {
	Zone      *Zone // Z2 HR
	ZoneTo    *Zone // Z2-Z3 HR (range end; nil if single zone)
	Percent   int   // 70% HR or 95% LTHR (0 if zone-based)
	PercentTo int   // 70-80% HR (range end, 0 if not range)
	IsLTHR    bool  // true for LTHR suffix
}

// PaceZoneTarget is a pace zone target, e.g. Z2 Pace, Z1-Z2 Pace.
type PaceZoneTarget struct {
	Zone   int // e.g. 2 for Z2 Pace
	ZoneTo int // 0 if single zone, else range end
}

// PacePercentTarget is a percent-of-pace target, e.g. 78-82% Pace.
type PacePercentTarget struct {
	Percent   int
	PercentTo int // 0 if single
}

// WattsTarget is an absolute wattage target, e.g. 220w or 200-240w.
type WattsTarget struct {
	Watts   int
	WattsTo int // 0 if single
}

// CustomZoneTarget is a custom power zone, e.g. CZ1, CZ2-CZ3.
type CustomZoneTarget struct {
	Zone   int
	ZoneTo int // 0 if single
}

// CadenceRange is an optional cadence target, e.g. 90rpm or 90-100rpm.
type CadenceRange struct {
	RPM   int
	RPMTo int // 0 if single
}

// Pace is a target running pace expressed as seconds per unit distance,
// or a range (e.g. 4:55-4:35 Pace).
type Pace struct {
	Seconds    int    // total seconds per unit for fast end (or single pace)
	SecondsEnd int    // slow end for range (0 if not a range; note: SecondsEnd > Seconds for slow-fast)
	Unit       string // "km" or "mi"
	Raw        string // canonical token, e.g. "3:55/km" or "4:55-4:35 Pace"
	IsPaceWord bool   // true when expressed as "4:00 Pace" or "4:55-4:35 Pace"
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
	case i.Pace != nil:
		return i.Pace.Raw
	case i.IsRecovery:
		return "intensity=recovery"
	case i.HR != nil:
		return i.HR.String()
	case i.PaceZone != nil:
		return i.PaceZone.String()
	case i.PacePercent != nil:
		return i.PacePercent.String()
	case i.Watts != nil:
		return i.Watts.String()
	case i.CustomZone != nil:
		return i.CustomZone.String()
	default:
		return ""
	}
}

func (h *HRTarget) String() string {
	suffix := "HR"
	if h.IsLTHR {
		suffix = "LTHR"
	}
	if h.Zone != nil {
		if h.ZoneTo != nil {
			return fmt.Sprintf("%s-%s %s", h.Zone, h.ZoneTo, suffix)
		}
		return fmt.Sprintf("%s %s", h.Zone, suffix)
	}
	if h.PercentTo != 0 {
		return fmt.Sprintf("%d-%d%% %s", h.Percent, h.PercentTo, suffix)
	}
	return fmt.Sprintf("%d%% %s", h.Percent, suffix)
}

func (p *PaceZoneTarget) String() string {
	if p.ZoneTo != 0 {
		return fmt.Sprintf("Z%d-Z%d Pace", p.Zone, p.ZoneTo)
	}
	return fmt.Sprintf("Z%d Pace", p.Zone)
}

func (p *PacePercentTarget) String() string {
	if p.PercentTo != 0 {
		return fmt.Sprintf("%d-%d%% Pace", p.Percent, p.PercentTo)
	}
	return fmt.Sprintf("%d%% Pace", p.Percent)
}

func (w *WattsTarget) String() string {
	if w.WattsTo != 0 {
		return fmt.Sprintf("%d-%dw", w.Watts, w.WattsTo)
	}
	return fmt.Sprintf("%dw", w.Watts)
}

func (c *CustomZoneTarget) String() string {
	if c.ZoneTo != 0 {
		return fmt.Sprintf("CZ%d-CZ%d", c.Zone, c.ZoneTo)
	}
	return fmt.Sprintf("CZ%d", c.Zone)
}
