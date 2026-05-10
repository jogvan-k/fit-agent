package render

import (
	"strings"
	"testing"
	"time"

	"github.com/jogvan-k/fit-agent/internal/icu"
)

func TestPulledWorkoutDayMarkdown(t *testing.T) {
	got, err := PulledWorkoutDayMarkdown(PulledWorkout{
		Event: icu.Event{
			ID:             4711,
			Category:       "WORKOUT",
			StartDateLocal: "2026-05-04T07:00:00",
			Name:           "Z2 Endurance",
			Type:           "Ride",
			MovingTime:     4500,
			Description:    "- 10m Z1\n- 60m Z2\n- 5m Z1",
		},
		Date:        time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 5, 3, 20, 14, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PulledWorkoutDayMarkdown: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"kind: pulled-workout-day",
		"read_only: true",
		"source: intervals.icu",
		"icu_event_id: 4711",
		"name: Z2 Endurance",
		"type: Ride",
		"moving_time_s: 4500",
		"# Z2 Endurance",
		"## ICU description",
		"- 60m Z2",
		"generated_at: 2026-05-03T20:14:00Z",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output:\n%s", want, s)
		}
	}
	// Must NOT contain a fit-workout fence — that's reserved for
	// agent-authored .md files.
	if strings.Contains(s, "```fit-workout") {
		t.Errorf("output should not contain a ```fit-workout``` fence:\n%s", s)
	}
}

func TestPulledWorkoutDayMarkdownRequiresIDAndDate(t *testing.T) {
	if _, err := PulledWorkoutDayMarkdown(PulledWorkout{Date: time.Now()}); err == nil {
		t.Errorf("expected error when ID is zero")
	}
	if _, err := PulledWorkoutDayMarkdown(PulledWorkout{Event: icu.Event{ID: 1}}); err == nil {
		t.Errorf("expected error when Date is zero")
	}
}
