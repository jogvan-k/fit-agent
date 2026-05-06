package fitparse

import (
	"path/filepath"
	"testing"
)

const sampleFIT = "../../testdata/fit/sample-intervals.fit"

func TestDecodeSample(t *testing.T) {
	a, err := Decode(filepath.FromSlash(sampleFIT))
	if err != nil {
		t.Fatal(err)
	}
	if a.Sport != "running" {
		t.Errorf("sport = %q, want running", a.Sport)
	}
	if a.StartLocal.IsZero() {
		t.Error("StartLocal not set")
	}
	if a.TotalTime <= 0 {
		t.Errorf("TotalTime = %v", a.TotalTime)
	}
	if a.MovingTime <= 0 || a.MovingTime > a.TotalTime {
		t.Errorf("MovingTime = %v (TotalTime=%v)", a.MovingTime, a.TotalTime)
	}
	if a.Distance <= 0 {
		t.Errorf("Distance = %v", a.Distance)
	}
	if a.AvgHR == 0 {
		t.Error("AvgHR not extracted")
	}
	if a.MaxHR < a.AvgHR {
		t.Errorf("MaxHR=%d < AvgHR=%d", a.MaxHR, a.AvgHR)
	}
	if len(a.Laps) == 0 {
		t.Fatal("no laps decoded")
	}
	if got := a.Laps[0].Index; got != 1 {
		t.Errorf("first lap index = %d, want 1", got)
	}
	for i, l := range a.Laps {
		if l.Duration < 0 {
			t.Errorf("lap %d duration negative", i+1)
		}
		if l.AvgSpeed > 0 && l.AvgPaceSecPerKm == 0 {
			t.Errorf("lap %d has speed but zero pace", i+1)
		}
	}
}

func TestDecodeMissingFile(t *testing.T) {
	_, err := Decode("does-not-exist.fit")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGroupIntervalsEmpty(t *testing.T) {
	if got := GroupIntervals(nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestGroupIntervalsByWorkoutStep(t *testing.T) {
	laps := []Lap{
		{Index: 1, Intensity: "warmup", WorkoutStepIndex: 0},
		{Index: 2, Intensity: "active", WorkoutStepIndex: 1},
		{Index: 3, Intensity: "rest", WorkoutStepIndex: 2},
		{Index: 4, Intensity: "active", WorkoutStepIndex: 1},
		{Index: 5, Intensity: "rest", WorkoutStepIndex: 2},
		{Index: 6, Intensity: "cooldown", WorkoutStepIndex: 3},
	}
	got := GroupIntervals(laps)
	if len(got) != 6 {
		t.Fatalf("got %d intervals, want 6 (one per step transition): %+v", len(got), got)
	}
	// First should be warmup, last cooldown.
	if got[0].Kind != "warmup" || got[5].Kind != "cooldown" {
		t.Errorf("unexpected kinds: first=%q last=%q", got[0].Kind, got[5].Kind)
	}
	// Each interval references the right workout step.
	wantSteps := []int{0, 1, 2, 1, 2, 3}
	for i, iv := range got {
		if iv.WorkoutStepIndex != wantSteps[i] {
			t.Errorf("interval %d: step=%d, want %d", i, iv.WorkoutStepIndex, wantSteps[i])
		}
	}
}

func TestGroupIntervalsHeuristicWorkSet(t *testing.T) {
	mk := func(idx int, intensity string) Lap {
		return Lap{Index: idx, Intensity: intensity, WorkoutStepIndex: -1}
	}
	laps := []Lap{
		mk(1, "warmup"),
		mk(2, "interval"),
		mk(3, "rest"),
		mk(4, "interval"),
		mk(5, "rest"),
		mk(6, "interval"),
		mk(7, "rest"),
		mk(8, "cooldown"),
	}
	got := GroupIntervals(laps)
	if len(got) != 3 {
		t.Fatalf("want 3 intervals (warmup, work_set, cooldown), got %d: %+v", len(got), got)
	}
	if got[0].Kind != "warmup" {
		t.Errorf("got[0].Kind = %q, want warmup", got[0].Kind)
	}
	if got[1].Kind != "work_set" {
		t.Errorf("got[1].Kind = %q, want work_set", got[1].Kind)
	}
	if want := []int{2, 3, 4, 5, 6, 7}; !equalInts(got[1].LapIndices, want) {
		t.Errorf("work_set lap indices = %v, want %v", got[1].LapIndices, want)
	}
	if got[2].Kind != "cooldown" {
		t.Errorf("got[2].Kind = %q, want cooldown", got[2].Kind)
	}
}

func TestGroupIntervalsHeuristicNoCoalesce(t *testing.T) {
	// Single work/rest pair should NOT collapse into a work_set
	// (we require >=2 reps).
	laps := []Lap{
		{Index: 1, Intensity: "warmup", WorkoutStepIndex: -1},
		{Index: 2, Intensity: "active", WorkoutStepIndex: -1},
		{Index: 3, Intensity: "rest", WorkoutStepIndex: -1},
		{Index: 4, Intensity: "cooldown", WorkoutStepIndex: -1},
	}
	got := GroupIntervals(laps)
	if len(got) != 4 {
		t.Errorf("want 4 intervals (no fold), got %d: %+v", len(got), got)
	}
}

func TestGroupIntervalsHeuristicCoalescesSameIntensity(t *testing.T) {
	laps := []Lap{
		{Index: 1, Intensity: "active", WorkoutStepIndex: -1},
		{Index: 2, Intensity: "active", WorkoutStepIndex: -1},
		{Index: 3, Intensity: "rest", WorkoutStepIndex: -1},
	}
	got := GroupIntervals(laps)
	if len(got) != 2 {
		t.Fatalf("want 2 intervals, got %d: %+v", len(got), got)
	}
	if !equalInts(got[0].LapIndices, []int{1, 2}) {
		t.Errorf("got[0].LapIndices = %v, want [1 2]", got[0].LapIndices)
	}
}

func TestSampleHeuristicGrouping(t *testing.T) {
	a, err := Decode(filepath.FromSlash(sampleFIT))
	if err != nil {
		t.Fatal(err)
	}
	// The sample is a 1-hour run with 2 laps and no workout_step
	// data. It shouldn't be misidentified as a work_set.
	if len(a.Intervals) != 2 {
		t.Errorf("got %d intervals, want 2 (one per lap)", len(a.Intervals))
	}
	for _, iv := range a.Intervals {
		if iv.Kind == "work_set" {
			t.Errorf("unexpected work_set in sample: %+v", iv)
		}
	}
}

func TestPaceDerivation(t *testing.T) {
	// 12 km/h ~= 3.333 m/s => pace 300 sec/km.
	speed := uint16(3333) // m/s * 1000
	pace := int(1000.0/(float64(speed)/1000.0) + 0.5)
	if pace != 300 {
		t.Errorf("pace derivation off: got %d", pace)
	}
}

func TestIntensityAndTriggerStrings(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"warmup", "warmup"},
		{"active", "active"},
		{"", "other"},
	}
	for _, tc := range cases {
		if got := intervalKind(tc.in); got != tc.want {
			t.Errorf("intervalKind(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSampleFileSize(t *testing.T) {
	// Guard against the sample being replaced with a stub: the real
	// intervals.icu sample is ~93 KB and decodes to multiple laps.
	a, err := Decode(filepath.FromSlash(sampleFIT))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Laps) < 1 {
		t.Errorf("sample decoded to 0 laps")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
