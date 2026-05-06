package render

import (
	"bytes"
	"fmt"
	"sort"
	"time"

	"github.com/jogvan-k/fit-agent/internal/icu"
)

// WellnessMonth is the input to [WellnessMonthYAML].
//
// Days are emitted in ascending date order regardless of input order.
type WellnessMonth struct {
	// Month is the calendar month the file represents; only the year
	// and month components are used.
	Month time.Time
	// GeneratedAt stamps the file header.
	GeneratedAt time.Time
	// Location is the athlete's local TZ; used for the
	// generated_at stamp formatting only (wellness day ids are
	// already date strings on the icu side).
	Location *time.Location
	// Days is the per-day wellness rows. Ids are expected to be
	// ISO calendar dates (YYYY-MM-DD).
	Days []icu.WellnessDay
}

// WellnessMonthYAML returns the YAML for one month of wellness data.
//
// The structure is:
//
//	# header comment block
//	month: 2026-05
//	generated_at: <ts>
//	days:
//	  "YYYY-MM-DD":
//	    resting_hr: ...
//	    ...
//
// Days with no data fields are still emitted (an empty map under the
// date key) so that the file remains a complete record of which dates
// were observed by the source.
func WellnessMonthYAML(m WellnessMonth) ([]byte, error) {
	loc := m.Location
	if loc == nil {
		loc = time.UTC
	}
	if m.Month.IsZero() {
		return nil, fmt.Errorf("WellnessMonth.Month is required")
	}

	days := append([]icu.WellnessDay(nil), m.Days...)
	sort.SliceStable(days, func(i, j int) bool { return days[i].ID < days[j].ID })

	var b bytes.Buffer
	b.WriteString("# Daily wellness for ")
	b.WriteString(m.Month.Format("2006-01"))
	b.WriteString(". Upserted by date on every `fit-agent fetch`.\n")
	b.WriteString("# Units: HR in bpm, sleep in hours (sleep_hours) and seconds\n")
	b.WriteString("# (sleep_seconds), weight in kg, stress unit-less, hrv (RMSSD) in ms.\n")
	b.WriteString("# Source of truth: ../.cache/wellness/")
	b.WriteString(m.Month.Format("2006-01"))
	b.WriteString(".json\n")
	fmt.Fprintf(&b, "month: %s\n", m.Month.Format("2006-01"))
	fmt.Fprintf(&b, "generated_at: %s\n", m.GeneratedAt.In(loc).Format(time.RFC3339))
	b.WriteString("days:\n")
	for _, d := range days {
		writeWellnessDay(&b, d)
	}
	return b.Bytes(), nil
}

func writeWellnessDay(b *bytes.Buffer, d icu.WellnessDay) {
	fmt.Fprintf(b, "  %q:\n", d.ID)
	if d.RestingHR > 0 {
		fmt.Fprintf(b, "    resting_hr: %d\n", d.RestingHR)
	}
	if d.HRV > 0 {
		fmt.Fprintf(b, "    hrv_rmssd: %s\n", formatFloat(d.HRV, 1))
	}
	if d.HRVSDNN > 0 {
		fmt.Fprintf(b, "    hrv_sdnn: %s\n", formatFloat(d.HRVSDNN, 1))
	}
	if d.Sleep > 0 {
		fmt.Fprintf(b, "    sleep_seconds: %d\n", int(d.Sleep))
		fmt.Fprintf(b, "    sleep_hours: %s\n", formatFloat(d.Sleep/3600.0, 2))
	}
	if d.SleepScore > 0 {
		fmt.Fprintf(b, "    sleep_score: %s\n", formatFloat(d.SleepScore, 1))
	}
	if d.SleepQuality > 0 {
		fmt.Fprintf(b, "    sleep_quality: %s\n", formatFloat(d.SleepQuality, 1))
	}
	if d.AvgSleepingHR > 0 {
		fmt.Fprintf(b, "    avg_sleeping_hr: %d\n", d.AvgSleepingHR)
	}
	if d.Steps > 0 {
		fmt.Fprintf(b, "    steps: %d\n", d.Steps)
	}
	if d.Weight > 0 {
		fmt.Fprintf(b, "    weight_kg: %s\n", formatFloat(d.Weight, 2))
	}
	if d.BodyFat > 0 {
		fmt.Fprintf(b, "    body_fat_pct: %s\n", formatFloat(d.BodyFat, 1))
	}
	if d.Stress > 0 {
		fmt.Fprintf(b, "    stress_avg: %s\n", formatFloat(d.Stress, 1))
	}
	if d.VO2Max > 0 {
		fmt.Fprintf(b, "    vo2max: %s\n", formatFloat(d.VO2Max, 1))
	}
	if d.CTL > 0 {
		fmt.Fprintf(b, "    ctl: %s\n", formatFloat(d.CTL, 1))
	}
	if d.ATL > 0 {
		fmt.Fprintf(b, "    atl: %s\n", formatFloat(d.ATL, 1))
	}
	if d.RampRate != 0 {
		fmt.Fprintf(b, "    ramp_rate: %s\n", formatFloat(d.RampRate, 2))
	}
	if d.Comments != "" {
		fmt.Fprintf(b, "    notes: %s\n", yamlBlockScalar(d.Comments, 4))
	}
}

// MergeWellnessDays upserts new entries onto a base, returning the
// merged sequence sorted by id ascending.
//
// "Upsert" means: for any id present in new, the new value wins
// completely (we do not field-merge, since intervals.icu always
// returns the full row). Ids present in base but absent from new are
// preserved.
//
// This is the helper a future fetch loop calls before re-rendering a
// month: read existing yaml → decode rows → upsert → re-render.
func MergeWellnessDays(base, fresh []icu.WellnessDay) []icu.WellnessDay {
	byID := make(map[string]icu.WellnessDay, len(base)+len(fresh))
	for _, d := range base {
		byID[d.ID] = d
	}
	for _, d := range fresh {
		byID[d.ID] = d
	}
	out := make([]icu.WellnessDay, 0, len(byID))
	for _, d := range byID {
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
