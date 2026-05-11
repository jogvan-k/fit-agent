package workoutdsl

import (
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
		{"bad duration", "- 5x Z2", `unexpected 'x'`}, // "x" isn't a duration unit
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
