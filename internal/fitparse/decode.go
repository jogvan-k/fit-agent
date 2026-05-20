// Package fitparse decodes intervals.icu .fit files into a small,
// agent-shaped struct. It wraps github.com/muktihari/fit and exposes
// only the per-lap and per-interval summaries that downstream renderers
// actually consume; per-second record streams stay in the .fit file
// (in .cache/) and can be added on demand.
package fitparse

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

// Record is a single per-second data point from the FIT record stream.
type Record struct {
	// Timestamp is the UTC time of this sample.
	Timestamp time.Time
	// Distance is cumulative distance from activity start, in meters.
	Distance float64
	// Speed is instantaneous speed in m/s. 0 when unavailable.
	Speed float64
	// HR is heart rate in bpm. 0 when unavailable.
	HR int
	// Cadence is cadence in rpm. 0 when unavailable.
	Cadence int
	// Altitude is altitude in meters above sea level. 0 when unavailable.
	Altitude float64
	// AltitudeValid indicates Altitude is a real value (not just zero).
	AltitudeValid bool
	// AltitudeIsBarometric is true when the altitude came from the
	// EnhancedAltitude field (barometric sensor). False means GPS-derived.
	// Barometric data is more accurate for relative elevation changes and
	// allows a tighter noise threshold in gain/loss computation.
	AltitudeIsBarometric bool
}


//
// All durations are time.Duration; distances are meters; speeds are m/s;
// pace is integer seconds per kilometer; HR is bpm; power is watts.
// Fields that the source FIT marked invalid are left at their zero value.
type ParsedActivity struct {
	// Sport is the activity sport (e.g. "running", "cycling"). Falls
	// back to the first lap's sport when the session sport is invalid.
	Sport string
	// StartLocal is the activity start time. The FIT spec stores UTC
	// in [time.Time]; intervals.icu downloads carry UTC. Renderers
	// project this into athlete-local TZ at output time.
	StartLocal time.Time
	// TotalTime is the elapsed duration including pauses (session
	// total_elapsed_time).
	TotalTime time.Duration
	// MovingTime is the timer duration excluding pauses
	// (session total_timer_time).
	MovingTime time.Duration
	// Distance is the total session distance in meters.
	Distance float64
	// AvgHR is the session-level average heart rate (bpm).
	AvgHR int
	// MaxHR is the session-level max heart rate (bpm).
	MaxHR int
	// AvgPower is the session-level average power (watts).
	AvgPower int
	// AvgCadence is the session-level average cadence (rpm).
	AvgCadence int
	// AvgSpeed is the session-level average speed in m/s.
	AvgSpeed float64
	// Calories is total session calories (kcal).
	Calories int
	// ElevationGain is total ascent for the session in meters.
	ElevationGain float64
	// ElevationLoss is total descent for the session in meters.
	ElevationLoss float64

	// Laps are the per-lap summaries in file order.
	Laps []Lap
	// Intervals groups laps by their workout step or by an
	// intensity-based heuristic. See [GroupIntervals].
	Intervals []Interval
	// Records are the per-second data points from the FIT record stream.
	// They are ordered by distance and can be used for granular segment stats.
	Records []Record
}

// Lap is a per-lap summary derived from the FIT file's lap messages.
type Lap struct {
	// Index is the 1-based lap number in file order.
	Index int
	// Trigger is the FIT lap_trigger as a lowercase string
	// ("manual", "time", "distance", "workout_step", ...).
	Trigger string
	// Intensity is the FIT intensity as a lowercase string
	// ("active", "rest", "warmup", "cooldown", "recovery",
	// "interval", "other").
	Intensity string
	// WorkoutStepIndex is the message index of the workout_step this
	// lap belongs to, or -1 when no step is associated.
	WorkoutStepIndex int
	// StartLocal is the lap start time (UTC; renderers reproject).
	StartLocal time.Time
	// Duration is the lap timer time (excludes pauses).
	Duration time.Duration
	// ElapsedTime is the lap elapsed time (includes pauses).
	ElapsedTime time.Duration
	// Distance is lap distance in meters.
	Distance float64
	// AvgHR is lap average heart rate (bpm). 0 when unavailable.
	AvgHR int
	// MaxHR is lap max heart rate (bpm). 0 when unavailable.
	MaxHR int
	// AvgPower is lap average power (watts). 0 when unavailable.
	AvgPower int
	// AvgCadence is lap average cadence (rpm). 0 when unavailable.
	AvgCadence int
	// AvgSpeed is lap average speed (m/s). 0 when unavailable.
	AvgSpeed float64
	// AvgPaceSecPerKm is derived from AvgSpeed; 0 when speed is
	// unavailable or zero.
	AvgPaceSecPerKm int
	// Calories burned during the lap (kcal). 0 when unavailable.
	Calories int
	// ElevationGain is total ascent for this lap in meters.
	ElevationGain float64
	// ElevationLoss is total descent for this lap in meters.
	ElevationLoss float64
}

// Interval is a group of laps that share an intent — either a
// workout_step (Garmin structured workout) or laps with the same
// intensity sequence.
type Interval struct {
	// Kind is the interval class derived from the underlying laps:
	// "warmup", "cooldown", "rest", "recovery", "active", "interval",
	// "work_set" (a repeating block of work+rest), or "other".
	Kind string
	// WorkoutStepIndex is the FIT workout_step message index, or -1
	// when the grouping was heuristic.
	WorkoutStepIndex int
	// LapIndices are 1-based indices into [ParsedActivity.Laps].
	LapIndices []int
}

// Decode parses the .fit file at path.
func Decode(path string) (*ParsedActivity, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return DecodeReader(f)
}

// DecodeReader parses a .fit stream from r.
//
// The whole file is decoded into memory; stream-decoding via
// [decoder.Decoder.Next] is not used because we need cross-message joins
// (laps to sessions, laps to workout steps).
func DecodeReader(r io.Reader) (*ParsedActivity, error) {
	d := decoder.New(r)
	fit, err := d.Decode()
	if err != nil {
		return nil, fmt.Errorf("decode fit: %w", err)
	}

	out := &ParsedActivity{}
	for i := range fit.Messages {
		m := &fit.Messages[i]
		switch m.Num {
		case typedef.MesgNumSession:
			applySession(out, mesgdef.NewSession(m))
		case typedef.MesgNumLap:
			out.Laps = append(out.Laps, lapFromMesg(len(out.Laps)+1, mesgdef.NewLap(m)))
		case typedef.MesgNumRecord:
			if rec, ok := recordFromMesg(mesgdef.NewRecord(m)); ok {
				out.Records = append(out.Records, rec)
			}
		}
	}

	out.Intervals = GroupIntervals(out.Laps)
	return out, nil
}

func applySession(out *ParsedActivity, s *mesgdef.Session) {
	if s.Sport != typedef.SportInvalid {
		out.Sport = s.Sport.String()
	}
	if !s.StartTime.IsZero() {
		out.StartLocal = s.StartTime
	}
	if s.TotalElapsedTime != basetype.Uint32Invalid {
		out.TotalTime = scaledMillisDuration(s.TotalElapsedTime)
	}
	if s.TotalTimerTime != basetype.Uint32Invalid {
		out.MovingTime = scaledMillisDuration(s.TotalTimerTime)
	}
	if s.TotalDistance != basetype.Uint32Invalid {
		out.Distance = float64(s.TotalDistance) / 100.0
	}
	if s.AvgHeartRate != basetype.Uint8Invalid {
		out.AvgHR = int(s.AvgHeartRate)
	}
	if s.MaxHeartRate != basetype.Uint8Invalid {
		out.MaxHR = int(s.MaxHeartRate)
	}
	if s.AvgPower != basetype.Uint16Invalid {
		out.AvgPower = int(s.AvgPower)
	}
	if s.AvgCadence != basetype.Uint8Invalid {
		out.AvgCadence = int(s.AvgCadence)
	}
	if s.AvgSpeed != basetype.Uint16Invalid {
		out.AvgSpeed = float64(s.AvgSpeed) / 1000.0
	}
	if s.TotalCalories != basetype.Uint16Invalid {
		out.Calories = int(s.TotalCalories)
	}
	if s.TotalAscent != basetype.Uint16Invalid {
		out.ElevationGain = float64(s.TotalAscent)
	}
	if s.TotalDescent != basetype.Uint16Invalid {
		out.ElevationLoss = float64(s.TotalDescent)
	}
}

func lapFromMesg(index int, l *mesgdef.Lap) Lap {
	out := Lap{
		Index:            index,
		WorkoutStepIndex: -1,
		Intensity:        intensityString(l.Intensity),
		Trigger:          lapTriggerString(l.LapTrigger),
	}
	if !l.StartTime.IsZero() {
		out.StartLocal = l.StartTime
	}
	if l.TotalTimerTime != basetype.Uint32Invalid {
		out.Duration = scaledMillisDuration(l.TotalTimerTime)
	}
	if l.TotalElapsedTime != basetype.Uint32Invalid {
		out.ElapsedTime = scaledMillisDuration(l.TotalElapsedTime)
	}
	if l.TotalDistance != basetype.Uint32Invalid {
		out.Distance = float64(l.TotalDistance) / 100.0
	}
	if l.AvgHeartRate != basetype.Uint8Invalid {
		out.AvgHR = int(l.AvgHeartRate)
	}
	if l.MaxHeartRate != basetype.Uint8Invalid {
		out.MaxHR = int(l.MaxHeartRate)
	}
	if l.AvgPower != basetype.Uint16Invalid {
		out.AvgPower = int(l.AvgPower)
	}
	if l.AvgCadence != basetype.Uint8Invalid {
		out.AvgCadence = int(l.AvgCadence)
	}
	if l.AvgSpeed != basetype.Uint16Invalid {
		out.AvgSpeed = float64(l.AvgSpeed) / 1000.0
		if out.AvgSpeed > 0 {
			out.AvgPaceSecPerKm = int(1000.0/out.AvgSpeed + 0.5)
		}
	}
	if l.TotalCalories != basetype.Uint16Invalid {
		out.Calories = int(l.TotalCalories)
	}
	if l.TotalAscent != basetype.Uint16Invalid {
		out.ElevationGain = float64(l.TotalAscent)
	}
	if l.TotalDescent != basetype.Uint16Invalid {
		out.ElevationLoss = float64(l.TotalDescent)
	}
	if l.WktStepIndex != typedef.MessageIndexInvalid {
		out.WorkoutStepIndex = int(l.WktStepIndex)
	}
	return out
}

// scaledMillisDuration converts a FIT scaled-by-1000 second value into a
// time.Duration. The raw value is "milliseconds" in the FIT sense
// (it's actually seconds * 1000).
func scaledMillisDuration(raw uint32) time.Duration {
	return time.Duration(raw) * time.Millisecond
}

func intensityString(i typedef.Intensity) string {
	if i == typedef.IntensityInvalid {
		return ""
	}
	return i.String()
}

func lapTriggerString(t typedef.LapTrigger) string {
	if t == typedef.LapTriggerInvalid {
		return ""
	}
	return t.String()
}

func recordFromMesg(r *mesgdef.Record) (Record, bool) {
	if r == nil || r.Distance == basetype.Uint32Invalid {
		return Record{}, false
	}
	rec := Record{
		Timestamp: r.Timestamp,
		Distance:  float64(r.Distance) / 100.0,
	}
	// Prefer enhanced fields (higher precision) when available.
	if r.EnhancedSpeed != basetype.Uint32Invalid {
		rec.Speed = float64(r.EnhancedSpeed) / 1000.0
	} else if r.Speed != basetype.Uint16Invalid {
		rec.Speed = float64(r.Speed) / 1000.0
	}
	if r.HeartRate != basetype.Uint8Invalid {
		rec.HR = int(r.HeartRate)
	}
	if r.Cadence != basetype.Uint8Invalid {
		rec.Cadence = int(r.Cadence)
	}
	if r.EnhancedAltitude != basetype.Uint32Invalid {
		rec.Altitude = float64(r.EnhancedAltitude)/5.0 - 500.0
		rec.AltitudeValid = true
		rec.AltitudeIsBarometric = true
	} else if r.Altitude != basetype.Uint16Invalid {
		rec.Altitude = float64(r.Altitude)/5.0 - 500.0
		rec.AltitudeValid = true
		rec.AltitudeIsBarometric = false
	}
	return rec, true
}
