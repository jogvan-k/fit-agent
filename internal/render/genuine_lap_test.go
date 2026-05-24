package render

import (
	"strings"
	"testing"
	"time"

	"github.com/jogvan-k/fit-agent/internal/fitparse"
	"github.com/jogvan-k/fit-agent/internal/icu"
)

// makeGenuineLapDay builds a minimal ActivityDay with a single genuine lap
// (no auto-splits) and FIT records that carry altitude data.
func makeGenuineLapDay(
	loc *time.Location,
	fitElevGain, fitElevLoss float64, // FIT lap message values (0 = missing)
	withAltRecords bool,
) ActivityDay {
	lapStart := time.Date(2024, 6, 1, 8, 0, 0, 0, loc)

	var records []fitparse.Record
	if withAltRecords {
		// Simulate a 500 m lap with a 20 m climb then 5 m descent:
		// altitudes: 100, 110, 120, 115  (barometric threshold=2 m)
		alts := []float64{100, 110, 120, 115}
		dists := []float64{0, 150, 350, 500}
		for i, alt := range alts {
			records = append(records, fitparse.Record{
				Timestamp:     lapStart.Add(time.Duration(i*30) * time.Second),
				Distance:      dists[i],
				Altitude:      alt,
				AltitudeValid: true,
			})
		}
	}

	return ActivityDay{
		Date:        time.Date(2024, 6, 1, 0, 0, 0, 0, loc),
		GeneratedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		Location:    loc,
		Activities: []ActivityInput{
			{
				Summary: icu.ActivitySummary{
					ID:         "i1",
					Name:       "Test Run",
					Type:       "Run",
					MovingTime: 120,
					Distance:   500.0,
				},
				FIT: &fitparse.ParsedActivity{
					HasBarometer: withAltRecords, // barometric when altitude records present
					Records: records,
					Laps: []fitparse.Lap{
						{
							Index:         1,
							Intensity:     "active",
							Distance:      500.0,
							Duration:      120 * time.Second,
							StartLocal:    lapStart,
							ElevationGain: fitElevGain,
							ElevationLoss: fitElevLoss,
						},
					},
				},
			},
		},
	}
}

// TestGenuineLap_FITElevationUsedWhenPresent checks that non-zero FIT lap
// elevation values are emitted as-is (existing behaviour preserved).
func TestGenuineLap_FITElevationUsedWhenPresent(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/London")
	day := makeGenuineLapDay(loc, 42.0, 5.0, false)
	got, err := ActivityDayYAML(day)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if !strings.Contains(out, "elevation_gain_m: 42.0") {
		t.Errorf("expected elevation_gain_m: 42.0 in output; got:\n%s", out)
	}
	if !strings.Contains(out, "elevation_loss_m: 5.0") {
		t.Errorf("expected elevation_loss_m: 5.0 in output; got:\n%s", out)
	}
}

// TestGenuineLap_RecordStreamFallback checks that when FIT elevation is zero
// but altitude records are present, the record-stream fallback is used.
func TestGenuineLap_RecordStreamFallback(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/London")
	day := makeGenuineLapDay(loc, 0, 0, true)
	got, err := ActivityDayYAML(day)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	// The altitude profile goes 100→110→120→115: gain should be detected
	// by the EWMA+hysteresis algorithm (threshold=2m barometric).
	if !strings.Contains(out, "elevation_gain_m:") {
		t.Errorf("expected elevation_gain_m from record-stream fallback; got:\n%s", out)
	}
	// Confirm fallback is used (gain should be non-zero, not equal to FIT value of 0).
	if strings.Contains(out, "elevation_gain_m: 0") {
		t.Errorf("elevation_gain_m should be non-zero from record-stream; got:\n%s", out)
	}
}

// TestGenuineLap_NoRecords checks that when both FIT elevation and records
// are absent, no elevation fields are emitted.
func TestGenuineLap_NoRecords(t *testing.T) {
	loc := mustLoadLocation(t, "Europe/London")
	day := makeGenuineLapDay(loc, 0, 0, false)
	got, err := ActivityDayYAML(day)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if strings.Contains(out, "elevation_gain_m:") {
		t.Errorf("expected no elevation_gain_m when no data; got:\n%s", out)
	}
	if strings.Contains(out, "elevation_loss_m:") {
		t.Errorf("expected no elevation_loss_m when no data; got:\n%s", out)
	}
}
