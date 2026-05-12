package plannedio

import (
	"strings"
	"testing"
)

const sampleDay = "---\n" +
	"fit-agent:\n" +
	"  kind: planned-workout-day\n" +
	"  date: 2026-05-04\n" +
	"workouts:\n" +
	"  - name: \"Z2 Endurance\"\n" +
	"    type: Ride\n" +
	"    moving_time_s: 4500\n" +
	"    icu_event_id: null\n" +
	"  - name: \"Strength\"\n" +
	"    type: WeightTraining\n" +
	"    icu_event_id: 12345\n" +
	"---\n" +
	"\n" +
	"## Z2 Endurance\n" +
	"\n" +
	"Easy aerobic ride. Stay strictly in Z2.\n" +
	"\n" +
	"```fit-workout\n" +
	"- 10m Z1\n" +
	"- 60m Z2\n" +
	"- 5m Z1\n" +
	"```\n" +
	"\n" +
	"## Strength\n" +
	"\n" +
	"Lower body session.\n" +
	"\n" +
	"```fit-workout\n" +
	"- 30m Z1\n" +
	"```\n"

func TestParseDay(t *testing.T) {
	d, err := ParseDay(sampleDay)
	if err != nil {
		t.Fatalf("ParseDay: %v", err)
	}
	if d.Date != "2026-05-04" {
		t.Errorf("date=%q", d.Date)
	}
	if len(d.Workouts) != 2 {
		t.Fatalf("workouts=%d", len(d.Workouts))
	}
	w := d.Workouts[0]
	if w.Meta.Name != "Z2 Endurance" || w.Meta.Type != "Ride" || w.Meta.MovingTimeS != 4500 {
		t.Errorf("meta: %+v", w.Meta)
	}
	if w.Meta.IcuEventID != nil {
		t.Errorf("expected nil event id, got %v", *w.Meta.IcuEventID)
	}
	if !strings.Contains(w.DSL, "- 60m Z2") {
		t.Errorf("dsl missing line: %q", w.DSL)
	}
	if !strings.Contains(w.Prose, "Easy aerobic ride") {
		t.Errorf("prose missing: %q", w.Prose)
	}
	w2 := d.Workouts[1]
	if w2.Meta.IcuEventID == nil || *w2.Meta.IcuEventID != 12345 {
		t.Errorf("event id: %+v", w2.Meta.IcuEventID)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	_, err := ParseDay("# just markdown\n")
	if err != nil {
		t.Errorf("expected nil error for missing frontmatter, got %v", err)
	}
}

func TestParseSectionMissing(t *testing.T) {
	src := "---\nfit-agent:\n  kind: planned-workout-day\n  date: 2026-05-04\nworkouts:\n  - name: Foo\n    type: Ride\n    icu_event_id: null\n---\n\n## Bar\n```fit-workout\n- 10m Z1\n```\n"
	_, err := ParseDay(src)
	if err == nil || !strings.Contains(err.Error(), "no matching") {
		t.Errorf("expected missing-section error, got %v", err)
	}
}

func TestStampEventIDReplacesNull(t *testing.T) {
	out, err := StampEventID(sampleDay, "Z2 Endurance", 99887)
	if err != nil {
		t.Fatalf("StampEventID: %v", err)
	}
	if !strings.Contains(out, "icu_event_id: 99887") {
		t.Errorf("missing stamped id; got:\n%s", out)
	}
	// Existing 12345 entry must remain untouched.
	if !strings.Contains(out, "icu_event_id: 12345") {
		t.Errorf("clobbered other id; got:\n%s", out)
	}
	// Re-parse to confirm structure is intact.
	d, err := ParseDay(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if d.Workouts[0].Meta.IcuEventID == nil || *d.Workouts[0].Meta.IcuEventID != 99887 {
		t.Errorf("re-parsed id: %+v", d.Workouts[0].Meta.IcuEventID)
	}
}

func TestStampEventIDInsertsWhenMissing(t *testing.T) {
	src := "---\nfit-agent:\n  kind: planned-workout-day\n  date: 2026-05-04\nworkouts:\n  - name: Foo\n    type: Ride\n---\n\n## Foo\n```fit-workout\n- 10m Z1\n```\n"
	out, err := StampEventID(src, "Foo", 42)
	if err != nil {
		t.Fatalf("StampEventID: %v", err)
	}
	if !strings.Contains(out, "icu_event_id: 42") {
		t.Errorf("missing inserted id:\n%s", out)
	}
}

func TestStampEventIDUnknownName(t *testing.T) {
	_, err := StampEventID(sampleDay, "Nonexistent", 1)
	if err == nil {
		t.Errorf("expected error for unknown workout name")
	}
}

func TestStampCompleted(t *testing.T) {
	src := `---
fit-agent:
  kind: planned-workout-day
  date: 2026-05-12
workouts:
  - name: "15 km Carrera larga"
    type: Run
    moving_time_s: 5400
    icu_event_id: 109278495
---

## 15 km Carrera larga

Easy run.
`
	got, err := StampCompleted(src)
	if err != nil {
		t.Fatalf("StampCompleted: %v", err)
	}
	if !strings.Contains(got, "completed: true") {
		t.Errorf("expected 'completed: true' in output:\n%s", got)
	}
	// Idempotent: stamping again should not duplicate.
	got2, err := StampCompleted(got)
	if err != nil {
		t.Fatalf("StampCompleted (2nd): %v", err)
	}
	if strings.Count(got2, "completed: true") != 1 {
		t.Errorf("expected exactly one 'completed: true', got:\n%s", got2)
	}
	// Prose and other fields preserved.
	if !strings.Contains(got, "icu_event_id: 109278495") {
		t.Errorf("icu_event_id lost")
	}
	if !strings.Contains(got, "Easy run.") {
		t.Errorf("prose lost")
	}
}
