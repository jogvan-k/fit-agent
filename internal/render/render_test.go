package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jogvan-k/fit-agent/internal/fitparse"
	"github.com/jogvan-k/fit-agent/internal/icu"
)

var update = flag.Bool("update", false, "regenerate golden files under testdata/workspace/")

// goldenAssert compares got against the file at path. When -update is
// passed, the file is (re)written instead.
func goldenAssert(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden parent: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run `go test ./internal/render -update` to create): %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("output mismatch for %s\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}

func TestActivityDayYAML_StrengthOnly(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/Copenhagen")
	day := ActivityDay{
		Date:        time.Date(2026, 5, 3, 0, 0, 0, 0, loc),
		GeneratedAt: time.Date(2026, 5, 3, 20, 14, 0, 0, time.UTC),
		Location:    loc,
		Activities: []ActivityInput{
			{
				Summary: icu.ActivitySummary{
					ID:             "i12346",
					Name:           "Evening Strength",
					Type:           "WeightTraining",
					StartDateLocal: "2026-05-03T18:00:00",
					ElapsedTime:    1800,
				},
			},
		},
	}
	got, err := ActivityDayYAML(day)
	if err != nil {
		t.Fatal(err)
	}
	goldenAssert(t, filepath.Join("testdata", "workspace", "activities", "2026-05-03-strength.yaml"), got)
}

func TestActivityDayYAML_RunWithFIT(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/Copenhagen")
	parsed, err := fitparse.Decode(filepath.Join("..", "..", "testdata", "fit", "sample-intervals.fit"))
	if err != nil {
		t.Fatalf("decode fit: %v", err)
	}
	day := ActivityDay{
		Date:        time.Date(2026, 5, 3, 0, 0, 0, 0, loc),
		GeneratedAt: time.Date(2026, 5, 3, 20, 14, 0, 0, time.UTC),
		Location:    loc,
		Activities: []ActivityInput{
			{
				Summary: icu.ActivitySummary{
					ID:                 "i55555",
					Name:               "Easy Run",
					Type:               "Run",
					StartDateLocal:     "2026-05-03T07:30:00",
					ElapsedTime:        3643,
					MovingTime:         3441,
					Distance:           10156.9,
					TotalElevationGain: 42.0,
					IcuTrainingLoad:    62.5,
					IcuRPE:             6,
					AverageHR:          145,
					MaxHR:              168,
					AverageSpeed:       2.952,
					Description:        "Felt good. Cool morning.",
				},
				FIT: parsed,
			},
		},
	}
	got, err := ActivityDayYAML(day)
	if err != nil {
		t.Fatal(err)
	}
	goldenAssert(t, filepath.Join("testdata", "workspace", "activities", "2026-05-03-run.yaml"), got)
}

func TestWellnessMonthYAML(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/Copenhagen")
	m := WellnessMonth{
		Month:       time.Date(2026, 5, 1, 0, 0, 0, 0, loc),
		GeneratedAt: time.Date(2026, 5, 3, 20, 14, 0, 0, time.UTC),
		Location:    loc,
		Days: []icu.WellnessDay{
			{ID: "2026-05-02", RestingHR: 50, HRV: 65, Sleep: 24480}, // 6.8h
			{ID: "2026-05-01", RestingHR: 48, HRV: 72, Sleep: 26640, SleepScore: 84, Steps: 8421, Weight: 74.2, Stress: 28},
			{ID: "2026-05-03", Comments: "Long ride day.\nLegs felt heavy."},
		},
	}
	got, err := WellnessMonthYAML(m)
	if err != nil {
		t.Fatal(err)
	}
	goldenAssert(t, filepath.Join("testdata", "workspace", "wellness", "2026-05.yaml"), got)
}

func TestPlannedWorkoutDayMarkdown(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/Copenhagen")
	day := PlannedWorkoutDay{
		Date: time.Date(2026, 5, 4, 0, 0, 0, 0, loc),
		Workouts: []PlannedWorkout{
			{
				Event: icu.Event{
					Name:           "Z2 Endurance",
					Type:           "Ride",
					Category:       "WORKOUT",
					StartDateLocal: "2026-05-04T07:00:00",
					MovingTime:     4500,
				},
				Notes: "Easy aerobic ride. Stay strictly in Z2; if HR drifts high\nin the second half, ease off rather than push through.",
				DSLBody: `- 10m Z1
- 60m Z2
- 5m Z1`,
			},
			{
				Event: icu.Event{
					ID:             123456,
					Name:           "Mobility",
					Type:           "Other",
					Category:       "WORKOUT",
					StartDateLocal: "2026-05-04T18:30:00",
					MovingTime:     1200,
				},
				DSLBody: `- 20m mobility flow`,
			},
		},
	}
	got, err := PlannedWorkoutDayMarkdown(day)
	if err != nil {
		t.Fatal(err)
	}
	goldenAssert(t, filepath.Join("testdata", "workspace", "planned-workouts", "2026-05-04.md"), got)
}

func TestMergeWellnessDays(t *testing.T) {
	base := []icu.WellnessDay{
		{ID: "2026-05-01", RestingHR: 48},
		{ID: "2026-05-02", RestingHR: 50},
	}
	fresh := []icu.WellnessDay{
		{ID: "2026-05-02", RestingHR: 51, Steps: 9000}, // overwrite
		{ID: "2026-05-03", RestingHR: 49},              // new
	}
	got := MergeWellnessDays(base, fresh)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].ID != "2026-05-01" || got[0].RestingHR != 48 {
		t.Errorf("base preserved: %+v", got[0])
	}
	if got[1].ID != "2026-05-02" || got[1].RestingHR != 51 || got[1].Steps != 9000 {
		t.Errorf("upsert wrong: %+v", got[1])
	}
	if got[2].ID != "2026-05-03" || got[2].RestingHR != 49 {
		t.Errorf("new missing: %+v", got[2])
	}
}

func TestYamlStringQuoting(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"", `""`},
		{"true", `"true"`},
		{"42", `"42"`},
		{"with space", "with space"},
		{" leading", `" leading"`},
		{"trailing ", `"trailing "`},
		{"a:b", `"a:b"`},
		{"-dash", `"-dash"`},
		{"line1\nline2", `"line1\nline2"`},
		{"emoji 🚴", "emoji 🚴"},
	}
	for _, tc := range cases {
		if got := yamlString(tc.in); got != tc.want {
			t.Errorf("yamlString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	cases := []struct {
		f    float64
		prec int
		want string
	}{
		{9800, 1, "9800.0"},
		{9800.123, 1, "9800.1"},
		{9800.156, 1, "9800.2"},
		{2.952, 3, "2.952"},
		{2.95200, 3, "2.952"},
		{2.0, 3, "2.0"},
	}
	for _, tc := range cases {
		if got := formatFloat(tc.f, tc.prec); got != tc.want {
			t.Errorf("formatFloat(%v, %d) = %s, want %s", tc.f, tc.prec, got, tc.want)
		}
	}
}

func TestICUBlockCompletedFlag(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/Madrid")
	date := time.Date(2026, 5, 12, 0, 0, 0, 0, loc)
	paired := "i147416102"
	block := ICUBlock(ICUBlockInput{
		Date: date,
		Events: []icu.Event{
			{
				ID:               109278495,
				Name:             "15 km Carrera larga",
				Type:             "Run",
				Category:         "WORKOUT",
				MovingTime:       4631,
				StartDateLocal:   "2026-05-12T00:00:00",
				PairedActivityID: paired,
			},
		},
	})
	s := string(block)
	if !strings.Contains(s, "completed: true") {
		t.Errorf("expected 'completed: true' in ICU block:\n%s", s)
	}
	if !strings.Contains(s, "icu_activity_id: "+paired) {
		t.Errorf("expected 'icu_activity_id: %s' in ICU block:\n%s", paired, s)
	}
}

func TestICUBlockNoCompletedFlagWhenUnpaired(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/Madrid")
	date := time.Date(2026, 5, 13, 0, 0, 0, 0, loc)
	block := ICUBlock(ICUBlockInput{
		Date: date,
		Events: []icu.Event{
			{
				ID:             109417301,
				Name:           "Repeticiones de 200",
				Type:           "Run",
				Category:       "WORKOUT",
				StartDateLocal: "2026-05-13T00:00:00",
			},
		},
	})
	s := string(block)
	if strings.Contains(s, "completed") {
		t.Errorf("expected no 'completed' in ICU block for unmatched event:\n%s", s)
	}
}
