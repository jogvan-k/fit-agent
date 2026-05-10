package icu

import "encoding/json"

// Athlete is the subset of /athlete/{id} we currently consume.
//
// intervals.icu returns ~50 fields here; we keep types loose (json.Number
// where useful) and decode-then-ignore unknown fields by virtue of the
// standard library's json behavior.
type Athlete struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email,omitempty"`
	Timezone  string  `json:"timezone,omitempty"`
	City      string  `json:"city,omitempty"`
	Country   string  `json:"country,omitempty"`
	Sex       string  `json:"sex,omitempty"`
	BirthDate string  `json:"icu_date_of_birth,omitempty"`
	Weight    float64 `json:"icu_weight,omitempty"`

	// Thresholds and zones (only the most useful for v1; agents read raw
	// JSON cache for the rest).
	FTP               int     `json:"icu_ftp,omitempty"`
	LTHR              int     `json:"icu_lthr,omitempty"`
	RestingHR         int     `json:"icu_resting_hr,omitempty"`
	MaxHR             int     `json:"icu_max_hr,omitempty"`
	ThresholdPace     float64 `json:"icu_threshold_pace,omitempty"` // m/s
	SwimThresholdPace float64 `json:"icu_swim_threshold_pace,omitempty"`

	// PowerZones, HRZones, PaceZones are intentionally left as raw
	// json.RawMessage on the cache side. Callers that need them today
	// can re-decode from .cache/athlete.json.
}

// ActivitySummary is the JSON shape returned in the activity list and
// detail endpoints. Only fields used by the renderer are typed today.
type ActivitySummary struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Description        string  `json:"description,omitempty"`
	Type               string  `json:"type"`
	StartDateLocal     string  `json:"start_date_local"`
	ElapsedTime        int     `json:"elapsed_time,omitempty"`
	MovingTime         int     `json:"moving_time,omitempty"`
	Distance           float64 `json:"distance,omitempty"`
	TotalElevationGain float64 `json:"total_elevation_gain,omitempty"`
	IcuTrainingLoad    float64 `json:"icu_training_load,omitempty"`
	IcuRPE             int     `json:"icu_rpe,omitempty"`
	AverageHR          int     `json:"average_heartrate,omitempty"`
	MaxHR              int     `json:"max_heartrate,omitempty"`
	AverageWatts       int     `json:"average_watts,omitempty"`
	MaxWatts           int     `json:"max_watts,omitempty"`
	AverageSpeed       float64 `json:"average_speed,omitempty"`
	MaxSpeed           float64 `json:"max_speed,omitempty"`
	// FileType is the source file type when intervals.icu has the
	// file (typically "fit", "tcx", "gpx"). Empty when no file is
	// associated.
	FileType string `json:"file_type,omitempty"`
	// Source carries the upload source (e.g. "STRAVA", "GARMIN");
	// useful for distinguishing manually-entered activities (no
	// file) from device uploads.
	Source string `json:"source,omitempty"`
}

// WellnessDay is a single daily wellness row.
//
// Field names follow intervals.icu's camelCase JSON. Numeric fields
// that the API may return as either integer or float are typed as
// float64 to accept both; renderers round to ints where appropriate.
type WellnessDay struct {
	ID            string  `json:"id"` // YYYY-MM-DD
	RestingHR     int     `json:"restingHR,omitempty"`
	HRV           float64 `json:"hrv,omitempty"`
	HRVSDNN       float64 `json:"hrvSDNN,omitempty"`
	Sleep         float64 `json:"sleepSecs,omitempty"` // seconds
	SleepScore    float64 `json:"sleepScore,omitempty"`
	SleepQuality  float64 `json:"sleepQuality,omitempty"`
	AvgSleepingHR int     `json:"avgSleepingHR,omitempty"`
	Steps         int     `json:"steps,omitempty"`
	Weight        float64 `json:"weight,omitempty"`
	BodyFat       float64 `json:"bodyFat,omitempty"`
	Stress        float64 `json:"stress,omitempty"`
	VO2Max        float64 `json:"vo2max,omitempty"`
	CTL           float64 `json:"ctl,omitempty"`
	ATL           float64 `json:"atl,omitempty"`
	RampRate      float64 `json:"rampRate,omitempty"`
	Comments      string  `json:"comments,omitempty"`
}

// Event is a planned workout (category=WORKOUT) on intervals.icu.
type Event struct {
	ID             int64  `json:"id,omitempty"`
	StartDateLocal string `json:"start_date_local"`
	Category       string `json:"category"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Type           string `json:"type,omitempty"`
	MovingTime     int    `json:"moving_time,omitempty"`
	IndoorOutdoor  string `json:"indoor,omitempty"`
	// WorkoutDoc is intervals.icu's structured workout representation.
	// Historically the API returned a string; in 2026 the field became a
	// JSON object ({"steps":[...]}). We keep it as raw JSON so decoding
	// tolerates both shapes; callers that need to interpret it should
	// unmarshal further. The CLI does not generate this field — it
	// pushes the DSL via [Event.Description] only.
	WorkoutDoc json.RawMessage `json:"workout_doc,omitempty"`
}
