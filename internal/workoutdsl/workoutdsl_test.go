package workoutdsl

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSimple(t *testing.T) {
	w, err := Parse("- 10m Z2\n- 30s Z5\n- 1h30m easy\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := len(w.Steps); got != 3 {
		t.Fatalf("steps=%d want 3", got)
	}
	if w.Steps[0].Simple == nil || w.Steps[0].Simple.Amount.Duration.Seconds != 600 {
		t.Errorf("step0: %+v", w.Steps[0].Simple)
	}
	if w.Steps[1].Simple.Amount.Duration.Seconds != 30 {
		t.Errorf("step1 seconds=%d", w.Steps[1].Simple.Amount.Duration.Seconds)
	}
	if w.Steps[2].Simple.Amount.Duration.Seconds != 90*60 {
		t.Errorf("step2 1h30m=%d", w.Steps[2].Simple.Amount.Duration.Seconds)
	}
	if w.Steps[2].Simple.Intensity.Named != "easy" {
		t.Errorf("step2 named=%q", w.Steps[2].Simple.Intensity.Named)
	}
}

func TestParseRepeat(t *testing.T) {
	w, err := Parse("- 5x (4m Z5 / 3m Z2)\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(w.Steps) != 1 || w.Steps[0].Repeat == nil {
		t.Fatalf("expected one repeat, got %+v", w.Steps)
	}
	r := w.Steps[0].Repeat
	if r.Reps != 5 {
		t.Errorf("reps=%d", r.Reps)
	}
	if r.Steps[0].Amount.Duration.Seconds != 240 || r.Steps[0].Intensity.Zone.N != 5 {
		t.Errorf("work=%+v", r.Steps[0])
	}
	if r.Steps[1].Amount.Duration.Seconds != 180 || r.Steps[1].Intensity.Zone.N != 2 {
		t.Errorf("rest=%+v", r.Steps[1])
	}
}

func TestParseRepeatWithDistance(t *testing.T) {
	w, err := Parse("- 8x (400m Z5 / 90s Z1)\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := w.Steps[0].Repeat
	if r.Steps[0].Amount.Distance == nil || r.Steps[0].Amount.Distance.Value != 400 || r.Steps[0].Amount.Distance.Unit != "m" {
		t.Errorf("work distance=%+v", r.Steps[0].Amount.Distance)
	}
}

func TestParseRamp(t *testing.T) {
	w, err := Parse("- 20m ramp Z1-Z3\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := w.Steps[0].Ramp
	if r == nil || r.From.N != 1 || r.To.N != 3 || r.Duration.Seconds != 1200 {
		t.Errorf("ramp=%+v", r)
	}
}

func TestParseNote(t *testing.T) {
	w, err := Parse("- 5m Z2 -- easy spin between sets\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if w.Steps[0].Note != "easy spin between sets" {
		t.Errorf("note=%q", w.Steps[0].Note)
	}
	if w.Steps[0].Simple.Note != "easy spin between sets" {
		t.Errorf("inner note=%q", w.Steps[0].Simple.Note)
	}
}

func TestParsePercent(t *testing.T) {
	w, err := Parse("- 15m 55%\n- 1m 150%\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p := w.Steps[0].Simple.Intensity.Percent; p == nil || *p != 55 {
		t.Errorf("first percent=%v", p)
	}
	if p := w.Steps[1].Simple.Intensity.Percent; p == nil || *p != 150 {
		t.Errorf("second percent=%v", p)
	}
}

func TestParseBlankAndComments(t *testing.T) {
	w, err := Parse("\n# warmup block\n- 10m Z1\n\n# main\n- 5x (4m Z5 / 3m Z2)\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(w.Steps) != 2 {
		t.Errorf("steps=%d", len(w.Steps))
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // substring of error
	}{
		{"no dash", "10m Z2", `must start with "- "`},
		{"empty step", "- ", "empty"},
		{"bad zone", "- 5m Z9", "invalid zone"},
		{"bad duration", "- 5x Z2", `no amount`}, // "x" isn't a duration unit
		{"unclosed repeat", "- 5x (4m Z5 / 3m Z2", "missing closing"},
		{"bad repeat split", "- 5x (4m Z5)", "work / rest"},
		{"unknown intensity", "- 5m foo", "unknown intensity"},
		{"bad ramp", "- 5m ramp Z1", "ramp range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%q want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	cases := []string{
		"- 10m Z2\n",
		"- 30s Z5\n- 1h30m easy\n- 5m Z1\n",
		"- 5x (4m Z5 / 3m Z2)\n",
		"- 8x (400m Z5 / 90s Z1)\n",
		"- 20m ramp Z1-Z3\n",
		"- 5m Z2 -- easy spin\n",
		"- 15m 55%\n",
		"- 3x (1m 150% / 1m 50%)\n",
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			w, err := Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out := RenderDSL(w)
			if out != src {
				t.Errorf("round-trip mismatch:\n got: %q\nwant: %q", out, src)
			}
			// Second pass must also be a fixed point.
			w2, err := Parse(out)
			if err != nil {
				t.Fatalf("Parse(out): %v", err)
			}
			if got := RenderDSL(w2); got != out {
				t.Errorf("second round-trip diverged:\n got: %q\nwant: %q", got, out)
			}
		})
	}
}

func TestRenderICU(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "single",
			src:  "- 10m Z2\n",
			want: "- 10m Z2\n",
		},
		{
			name: "cookbook example",
			src:  "- 15m 55% -- Warmup\n- 3x (1m 150% / 1m 50%)\n- 5m 50%\n- 5m 120%\n- 15m 55%\n",
			want: "- 15m 55% Warmup\n\n3x\n- 1m 150%\n- 1m 50%\n\n- 5m 50%\n- 5m 120%\n- 15m 55%\n",
		},
		{
			name: "ramp",
			src:  "- 20m ramp Z1-Z3\n",
			want: "- 20m ramp Z1-Z3\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := RenderICU(w)
			if got != tc.want {
				t.Errorf("RenderICU mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	w, err := Parse("- 10m Z1\n- 5x (4m Z5 / 3m Z2)\n- 5m Z1\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	s := Summary(w)
	wantSecs := 10*60 + 5*(4*60+3*60) + 5*60
	if s.TotalSeconds != wantSecs {
		t.Errorf("TotalSeconds=%d want %d", s.TotalSeconds, wantSecs)
	}
	if s.Steps != 3 {
		t.Errorf("Steps=%d", s.Steps)
	}
	if s.ZoneTargets != 4 {
		t.Errorf("ZoneTargets=%d", s.ZoneTargets)
	}
}

func TestParseRepeatWithInnerNote(t *testing.T) {
	w, err := Parse("- 5x (4m Z5 -- hold steady / 3m Z2 -- recover)\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := w.Steps[0].Repeat
	if r == nil {
		t.Fatalf("expected repeat, got %+v", w.Steps[0])
	}
	if r.Steps[0].Note != "hold steady" {
		t.Errorf("work note=%q", r.Steps[0].Note)
	}
	if r.Steps[1].Note != "recover" {
		t.Errorf("rest note=%q", r.Steps[1].Note)
	}
	// Round-trip.
	out := RenderDSL(w)
	want := "- 5x (4m Z5 -- hold steady / 3m Z2 -- recover)\n"
	if out != want {
		t.Errorf("round-trip:\n got: %q\nwant: %q", out, want)
	}
}

func TestParseRepeatWithOuterNote(t *testing.T) {
	w, err := Parse("- 5x (4m Z5 / 3m Z2) -- main set\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if w.Steps[0].Note != "main set" {
		t.Errorf("outer note=%q", w.Steps[0].Note)
	}
	if w.Steps[0].Repeat == nil {
		t.Fatalf("expected repeat")
	}
}

func TestParseDistanceEdgeCases(t *testing.T) {
	// "5m" should be minutes (duration), "400m" should be metres.
	w, err := Parse("- 5m Z1\n- 400m Z5\n- 5km Z3\n- 100y easy\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if w.Steps[0].Simple.Amount.Duration == nil {
		t.Errorf("5m should be duration")
	}
	if d := w.Steps[1].Simple.Amount.Distance; d == nil || d.Value != 400 || d.Unit != "m" {
		t.Errorf("400m: %+v", d)
	}
	if d := w.Steps[2].Simple.Amount.Distance; d == nil || d.Value != 5 || d.Unit != "km" {
		t.Errorf("5km: %+v", d)
	}
	if d := w.Steps[3].Simple.Amount.Distance; d == nil || d.Value != 100 || d.Unit != "y" {
		t.Errorf("100y: %+v", d)
	}
}

func TestParseBlockRepeatThreeSteps(t *testing.T) {
	src := "4x\n- 1km threshold\n- 200m Z5\n- 120s recovery\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(w.Steps) != 1 || w.Steps[0].Repeat == nil {
		t.Fatalf("expected one repeat, got %+v", w.Steps)
	}
	r := w.Steps[0].Repeat
	if r.Reps != 4 {
		t.Errorf("reps=%d want 4", r.Reps)
	}
	if len(r.Steps) != 3 {
		t.Fatalf("steps=%d want 3", len(r.Steps))
	}
	if d := r.Steps[0].Amount.Distance; d == nil || d.Value != 1 || d.Unit != "km" {
		t.Errorf("step0 distance=%+v", d)
	}
	if r.Steps[0].Intensity.Named != "threshold" {
		t.Errorf("step0 intensity=%+v", r.Steps[0].Intensity)
	}
	if d := r.Steps[1].Amount.Distance; d == nil || d.Value != 200 || d.Unit != "m" {
		t.Errorf("step1 distance=%+v", d)
	}
	if r.Steps[2].Amount.Duration == nil || r.Steps[2].Amount.Duration.Seconds != 120 {
		t.Errorf("step2 duration=%+v", r.Steps[2].Amount)
	}
	if r.Steps[2].Intensity.Named != "recovery" {
		t.Errorf("step2 intensity=%+v", r.Steps[2].Intensity)
	}
}

func TestParseBlockRepeatTerminatedByBlankLine(t *testing.T) {
	src := "- 2km easy\n\n4x\n- 1km threshold\n- 200m Z5\n- 120s recovery\n\n- 1km easy\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(w.Steps) != 3 {
		t.Fatalf("steps=%d want 3", len(w.Steps))
	}
	if w.Steps[0].Simple == nil {
		t.Errorf("step0 should be simple, got %+v", w.Steps[0])
	}
	if w.Steps[1].Repeat == nil || len(w.Steps[1].Repeat.Steps) != 3 {
		t.Errorf("step1 should be 3-step repeat, got %+v", w.Steps[1])
	}
	if w.Steps[2].Simple == nil {
		t.Errorf("step2 should be simple, got %+v", w.Steps[2])
	}
}

func TestParseBlockRepeatTooShort(t *testing.T) {
	// A block-form Nx header followed by only one step is an error;
	// repeats need at least two sub-steps to be meaningful.
	_, err := Parse("3x\n- 1km Z5\n")
	if err == nil {
		t.Fatalf("expected error for single-step repeat block")
	}
	if !strings.Contains(err.Error(), "at least 2 steps") {
		t.Errorf("err=%q", err.Error())
	}
}

func TestParseBlockRepeatWithOuterNote(t *testing.T) {
	src := "3x -- main set\n- 1km threshold\n- 200m Z5\n- 90s recovery\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if w.Steps[0].Note != "main set" {
		t.Errorf("note=%q", w.Steps[0].Note)
	}
	if w.Steps[0].Repeat == nil || len(w.Steps[0].Repeat.Steps) != 3 {
		t.Fatalf("expected 3-step repeat")
	}
}

func TestRoundTripBlockRepeat(t *testing.T) {
	// Multi-step repeats round-trip via the block form.
	src := "3x\n- 1km threshold\n- 200m Z5\n- 90s recovery\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := RenderDSL(w)
	if out != src {
		t.Errorf("round-trip mismatch:\n got: %q\nwant: %q", out, src)
	}
	w2, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse(out): %v", err)
	}
	if got := RenderDSL(w2); got != out {
		t.Errorf("second round-trip diverged:\n got: %q\nwant: %q", got, out)
	}
}

func TestRenderICUBlockRepeat(t *testing.T) {
	// 200m should be emitted as 200mtr in ICU output so intervals.icu
	// doesn't misinterpret it as 200 minutes.
	src := "3x\n- 1km threshold\n- 200m Z5\n- 90s recovery\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := RenderICU(w)
	want := "3x\n- 1km threshold\n- 200mtr Z5\n- 90s recovery\n"
	if got != want {
		t.Errorf("RenderICU mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderICUMetresSuffix(t *testing.T) {
	// Bare 'm' distances must be emitted as 'mtr' in ICU output.
	// intervals.icu treats 'm' as minutes; 'mtr' is the unambiguous metres suffix.
	cases := []struct {
		src  string
		want string
	}{
		{"- 400m Z5\n", "- 400mtr Z5\n"},
		{"- 300m threshold\n", "- 300mtr threshold\n"},
		// km and y should pass through unchanged.
		{"- 1km easy\n", "- 1km easy\n"},
		{"- 100y easy\n", "- 100y easy\n"},
		// Duration 'm' (minutes) must not be affected.
		{"- 5m easy\n", "- 5m easy\n"},
	}
	for _, tc := range cases {
		w, err := Parse(tc.src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.src, err)
		}
		got := RenderICU(w)
		if got != tc.want {
			t.Errorf("src=%q\n got: %q\nwant: %q", tc.src, got, tc.want)
		}
	}
}

func TestParsePace(t *testing.T) {
	cases := []struct {
		src      string
		wantRaw  string
		wantSecs int
		wantUnit string
	}{
		{"- 1km 3:55/km\n", "3:55/km", 235, "km"},
		{"- 200m 3:30/km\n", "3:30/km", 210, "km"},
		{"- 5km 4:15/km\n", "4:15/km", 255, "km"},
		{"- 1km 3:55\n", "3:55/km", 235, "km"}, // implicit /km
		{"- 1600m 6:30/mi\n", "6:30/mi", 390, "mi"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			w, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			p := w.Steps[0].Simple.Intensity.Pace
			if p == nil {
				t.Fatalf("expected Pace intensity, got %+v", w.Steps[0].Simple.Intensity)
			}
			if p.Raw != tc.wantRaw {
				t.Errorf("Raw=%q want %q", p.Raw, tc.wantRaw)
			}
			if p.Seconds != tc.wantSecs {
				t.Errorf("Seconds=%d want %d", p.Seconds, tc.wantSecs)
			}
			if p.Unit != tc.wantUnit {
				t.Errorf("Unit=%q want %q", p.Unit, tc.wantUnit)
			}
		})
	}
}

func TestPaceRoundTrip(t *testing.T) {
	cases := []string{
		"- 1km 3:55/km\n",
		"- 200m 3:30/km\n",
		"- 1600m 6:30/mi\n",
		"- 1km 3:55/km -- aim for negative split\n",
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			w, err := Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out := RenderDSL(w)
			if out != src {
				t.Errorf("round-trip:\n got: %q\nwant: %q", out, src)
			}
		})
	}
}

func TestPaceRenderICU(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{
			// Simple pace step.
			"- 1km 3:55/km\n",
			"- 1km 3:55/km\n",
		},
		{
			// Metres convert to mtr.
			"- 200m 3:30/km\n",
			"- 200mtr 3:30/km\n",
		},
		{
			// Block repeat with pace targets — mirrors Repeticiones de 200 workout.
			"4x\n- 1km 3:55/km\n- 200m 3:30/km\n- 2m recovery\n",
			"4x\n- 1km 3:55/km\n- 200mtr 3:30/km\n- 2m recovery\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			w, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := RenderICU(w)
			if got != tc.want {
				t.Errorf("RenderICU:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestPaceInRepeat(t *testing.T) {
	// Inline repeat with pace.
	w, err := Parse("- 4x (1km 3:55/km / 2m recovery)\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := w.Steps[0].Repeat
	if r == nil {
		t.Fatalf("expected repeat")
	}
	if r.Steps[0].Intensity.Pace == nil || r.Steps[0].Intensity.Pace.Raw != "3:55/km" {
		t.Errorf("work pace=%+v", r.Steps[0].Intensity)
	}
}

func TestPaceInvalidInputs(t *testing.T) {
	// A token that looks like a pace but has bad seconds (>= 60) should
	// fall through to "unknown intensity".
	_, err := Parse("- 1km 3:60/km\n")
	if err == nil {
		t.Fatalf("expected error for 3:60/km")
	}
	if !strings.Contains(err.Error(), "unknown intensity") {
		t.Errorf("err=%q", err.Error())
	}
}

func TestRenderWorkoutDoc(t *testing.T) {
	src := "4x\n- 1km 3:55/km\n- 200m 3:30/km\n- 2m recovery\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	raw, err := RenderWorkoutDoc(w)
	if err != nil {
		t.Fatalf("RenderWorkoutDoc: %v", err)
	}
	var doc WorkoutDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Steps) != 1 {
		t.Fatalf("steps=%d want 1", len(doc.Steps))
	}
	rep := doc.Steps[0]
	if rep.Reps != 4 {
		t.Errorf("reps=%d want 4", rep.Reps)
	}
	if len(rep.Steps) != 3 {
		t.Fatalf("sub-steps=%d want 3", len(rep.Steps))
	}
	// 1km at 3:55/km = 235 secs/km
	work := rep.Steps[0]
	if work.Distance != 1000 {
		t.Errorf("work.Distance=%d want 1000", work.Distance)
	}
	if work.Pace == nil || work.Pace.Units != "secs_km" || work.Pace.Value != 235 {
		t.Errorf("work.Pace=%+v", work.Pace)
	}
	if work.Duration != 235 { // 1000m * 235 / 1000
		t.Errorf("work.Duration=%d want 235", work.Duration)
	}
	// 200m at 3:30/km = 210 secs/km
	kick := rep.Steps[1]
	if kick.Distance != 200 {
		t.Errorf("kick.Distance=%d want 200", kick.Distance)
	}
	if kick.Pace == nil || kick.Pace.Value != 210 {
		t.Errorf("kick.Pace=%+v", kick.Pace)
	}
	if kick.Duration != 42 { // 200 * 210 / 1000
		t.Errorf("kick.Duration=%d want 42", kick.Duration)
	}
	// recovery: 2m duration, no pace target
	rec := rep.Steps[2]
	if rec.Duration != 120 {
		t.Errorf("rec.Duration=%d want 120", rec.Duration)
	}
	if rec.Pace != nil {
		t.Errorf("rec.Pace should be nil, got %+v", rec.Pace)
	}
}

func TestHasTargets(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"- 1km 3:55/km\n", true},
		{"- 10m Z2\n", true},
		{"- 15m 55%\n", true},
		{"- 11km easy\n", false},
		{"- 2km recovery\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			w, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := HasTargets(w); got != tc.want {
				t.Errorf("HasTargets=%v want %v", got, tc.want)
			}
		})
	}
}

func TestSummaryBlockRepeat(t *testing.T) {
	// 4x of (60s + 30s + 120s) = 4 * 210 = 840s.
	w, err := Parse("4x\n- 60s Z5\n- 30s Z5\n- 120s recovery\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	s := Summary(w)
	if s.TotalSeconds != 4*(60+30+120) {
		t.Errorf("TotalSeconds=%d want %d", s.TotalSeconds, 4*(60+30+120))
	}
}

// TestRenderICUFullSyntax tests round-trip parse→renderICU for the full ICU syntax.
func TestRenderICUFullSyntax(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "easy run km Z2 Pace",
			src:  "- 11km Z2 Pace\n",
			want: "- 11km Z2 Pace\n",
		},
		{
			name: "pace zone range",
			src:  "- 3.2km Z1-Z2 Pace\n",
			want: "- 3.2km Z1-Z2 Pace\n",
		},
		{
			name: "label with absolute pace",
			src:  "- Run high 300mtr 4:00 Pace\n",
			want: "- Run high 300mtr 4:00 Pace\n",
		},
		{
			name: "label with absolute pace range",
			src:  "- Run low 300mtr 4:55-4:35 Pace\n",
			want: "- Run low 300mtr 4:55-4:35 Pace\n",
		},
		{
			name: "intensity=recovery",
			src:  "- 90s intensity=recovery\n",
			want: "- 90s intensity=recovery\n",
		},
		{
			name: "Z2 HR",
			src:  "- 10m Z2 HR\n",
			want: "- 10m Z2 HR\n",
		},
		{
			name: "HR percent range",
			src:  "- 30m 70-80% HR\n",
			want: "- 30m 70-80% HR\n",
		},
		{
			name: "LTHR percent",
			src:  "- 10m 95% LTHR\n",
			want: "- 10m 95% LTHR\n",
		},
		{
			name: "watts",
			src:  "- 5m 220w\n",
			want: "- 5m 220w\n",
		},
		{
			name: "watts range",
			src:  "- 5m 200-240w\n",
			want: "- 5m 200-240w\n",
		},
		{
			name: "custom zone",
			src:  "- 10m CZ2\n",
			want: "- 10m CZ2\n",
		},
		{
			name: "cadence",
			src:  "- 10m 75% 90rpm\n",
			want: "- 10m 75% 90rpm\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			got := RenderICU(w)
			if got != tc.want {
				t.Errorf("RenderICU mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestRepeatWithLabel tests that block repeats with labels parse and render correctly.
func TestRepeatWithLabel(t *testing.T) {
	src := "Main Set 4x\n- 1km 3:55/km\n- 200m recovery\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(w.Steps) != 1 || w.Steps[0].Repeat == nil {
		t.Fatalf("expected one repeat, got %+v", w.Steps)
	}
	r := w.Steps[0].Repeat
	if r.Label != "Main Set" {
		t.Errorf("label=%q want %q", r.Label, "Main Set")
	}
	if r.Reps != 4 {
		t.Errorf("reps=%d want 4", r.Reps)
	}
	// ICU render should emit "Main Set 4x"
	icu := RenderICU(w)
	if !strings.Contains(icu, "Main Set 4x") {
		t.Errorf("ICU output missing label:\n%q", icu)
	}
}

// TestPaceZoneKmAmount tests that km distances with pace zone work.
func TestPaceZoneKmAmount(t *testing.T) {
	src := "- 11km Z2 Pace\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	step := w.Steps[0].Simple
	if step == nil {
		t.Fatalf("expected simple step")
	}
	if step.Intensity.PaceZone == nil {
		t.Fatalf("expected PaceZone intensity, got %+v", step.Intensity)
	}
	if step.Intensity.PaceZone.Zone != 2 {
		t.Errorf("zone=%d want 2", step.Intensity.PaceZone.Zone)
	}
}

// TestMarkdownSkip tests that markdown formatting lines are silently skipped.
func TestMarkdownSkip(t *testing.T) {
	src := "---\n**Main workout**\n| col1 | col2 |\n- 10m Z2\n"
	w, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(w.Steps) != 1 {
		t.Errorf("steps=%d want 1", len(w.Steps))
	}
}
