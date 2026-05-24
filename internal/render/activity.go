// Package render turns intervals.icu JSON and parsed FIT data into
// the agent-facing files the plan describes (§4, §10): YAML for
// activities and wellness, markdown for planned workouts.
//
// Output is hand-emitted rather than marshalled from a struct, for two
// reasons:
//
//  1. The YAML data files require a header comment block describing
//     units; gopkg.in/yaml.v3 supports comments via Node, but the
//     resulting code is harder to read than a small buffered writer.
//  2. Field ordering, omission rules, and the multi-doc layout matter
//     for diff stability and for golden tests; explicit emission keeps
//     them all in one place.
//
// Time policy: callers pass a *time.Location representing the
// athlete's local timezone; renderers project all UTC FIT timestamps
// into that zone before formatting as ISO-8601 with offset.
package render

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jogvan-k/fit-agent/internal/fitparse"
	"github.com/jogvan-k/fit-agent/internal/icu"
)

// ActivityDay is the input to [ActivityDayYAML].
//
// Activities are rendered in start-time order regardless of input order.
type ActivityDay struct {
	// Date is the local calendar date the file represents.
	Date time.Time
	// GeneratedAt stamps the file header; pass time.Now in production
	// and a fixed value in tests.
	GeneratedAt time.Time
	// Location is the athlete's local TZ; UTC timestamps are
	// projected into this zone for output.
	Location *time.Location
	// Activities are the per-activity inputs. Each pairs an
	// intervals.icu summary with the parsed FIT (when available).
	Activities []ActivityInput
	// AutoSplitDistanceM is the threshold in metres for implicit lap
	// splitting. Active laps longer than this value are divided into
	// consecutive segments of this distance (plus a remainder tail).
	// 0 disables the feature.
	AutoSplitDistanceM int
}

// ActivityInput pairs the icu summary with the parsed FIT.
//
// FIT may be nil when the activity has no FIT file (e.g. manual
// strength entry); in that case only the icu-side fields are emitted.
type ActivityInput struct {
	Summary icu.ActivitySummary
	FIT     *fitparse.ParsedActivity
}

// ActivityDayYAML returns the multi-document YAML for one calendar
// day's activities.
//
// The output starts with a header comment describing units and the
// cache path, then a top-level metadata document, then one
// activity document per item separated by `---` lines.
func ActivityDayYAML(day ActivityDay) ([]byte, error) {
	loc := day.Location
	if loc == nil {
		loc = time.UTC
	}
	if day.Date.IsZero() {
		return nil, fmt.Errorf("ActivityDay.Date is required")
	}

	acts := append([]ActivityInput(nil), day.Activities...)
	sort.SliceStable(acts, func(i, j int) bool {
		return acts[i].Summary.StartDateLocal < acts[j].Summary.StartDateLocal
	})

	var b bytes.Buffer
	writeActivityHeader(&b, day, loc)

	for _, a := range acts {
		b.WriteString("---\n")
		writeActivityDoc(&b, a, loc, day.AutoSplitDistanceM)
	}
	return b.Bytes(), nil
}

func writeActivityHeader(b *bytes.Buffer, day ActivityDay, loc *time.Location) {
	b.WriteString("# fit-agent activity day. Regenerated on every `fit-agent fetch`.\n")
	b.WriteString("# Units: time in seconds (HH:MM:SS where labeled), distance in meters,\n")
	b.WriteString("# speed in m/s, pace in sec/km, power in watts, HR in bpm.\n")
	b.WriteString("# Source of truth: ../.cache/activities/<icu_id>.{json,fit}\n")
	fmt.Fprintf(b, "date: %s\n", day.Date.Format("2006-01-02"))
	fmt.Fprintf(b, "generated_at: %s\n", day.GeneratedAt.In(loc).Format(time.RFC3339))
	b.WriteString("source: intervals.icu\n")
}

func writeActivityDoc(b *bytes.Buffer, a ActivityInput, loc *time.Location, autoSplitM int) {
	s := a.Summary
	fmt.Fprintf(b, "icu_id: %s\n", yamlString(s.ID))
	fmt.Fprintf(b, "name: %s\n", yamlString(s.Name))
	fmt.Fprintf(b, "type: %s\n", yamlString(s.Type))
	if s.StartDateLocal != "" {
		fmt.Fprintf(b, "start_local: %s\n", yamlString(s.StartDateLocal))
	}
	if s.ElapsedTime > 0 {
		fmt.Fprintf(b, "duration_s: %d\n", s.ElapsedTime)
	}
	if s.MovingTime > 0 {
		fmt.Fprintf(b, "moving_time_s: %d\n", s.MovingTime)
	}
	if s.Distance > 0 {
		fmt.Fprintf(b, "distance_m: %s\n", formatFloat(s.Distance, 1))
	}
	if s.TotalElevationGain > 0 {
		fmt.Fprintf(b, "elevation_gain_m: %s\n", formatFloat(s.TotalElevationGain, 1))
	}
	// Elevation loss comes from FIT session data when available.
	if a.FIT != nil && a.FIT.ElevationLoss > 0 {
		fmt.Fprintf(b, "elevation_loss_m: %s\n", formatFloat(a.FIT.ElevationLoss, 1))
	}
	if s.IcuTrainingLoad > 0 {
		fmt.Fprintf(b, "tss: %s\n", formatFloat(s.IcuTrainingLoad, 1))
	}
	if s.IcuRPE > 0 {
		fmt.Fprintf(b, "rpe: %d\n", s.IcuRPE)
	}
	if s.AverageHR > 0 {
		fmt.Fprintf(b, "avg_hr: %d\n", s.AverageHR)
	}
	if s.MaxHR > 0 {
		fmt.Fprintf(b, "max_hr: %d\n", s.MaxHR)
	}
	if s.AverageWatts > 0 {
		fmt.Fprintf(b, "avg_power: %d\n", s.AverageWatts)
	}
	if s.MaxWatts > 0 {
		fmt.Fprintf(b, "max_power: %d\n", s.MaxWatts)
	}
	if s.AverageSpeed > 0 {
		fmt.Fprintf(b, "avg_speed_mps: %s\n", formatFloat(s.AverageSpeed, 3))
	}
	if s.Description != "" {
		fmt.Fprintf(b, "athlete_notes: %s\n", yamlBlockScalar(s.Description, 0))
	} else {
		b.WriteString("athlete_notes: \"\"\n")
	}
	if s.HasWeather {
		writeWeather(b, s)
	}

	if a.FIT == nil {
		return
	}
	// Wind direction for per-segment wind stats (nil when unavailable).
	var windDeg *int
	if s.HasWeather && s.PrevailingWindDeg != nil {
		windDeg = s.PrevailingWindDeg
	}
	if len(a.FIT.Laps) > 0 {
		b.WriteString("laps:\n")
		for _, l := range a.FIT.Laps {
			writeLap(b, l, loc, autoSplitM, a.FIT.Records, windDeg, a.FIT.HasBarometer)
		}
	}
	if len(a.FIT.Intervals) > 0 {
		b.WriteString("intervals:\n")
		for _, iv := range a.FIT.Intervals {
			writeInterval(b, iv)
		}
	}
}

func writeLap(b *bytes.Buffer, l fitparse.Lap, loc *time.Location, autoSplitM int, records []fitparse.Record, windDeg *int, hasBarometer bool) {
	fmt.Fprintf(b, "  - i: %d\n", l.Index)
	if l.Intensity != "" {
		fmt.Fprintf(b, "    type: %s\n", yamlString(l.Intensity))
	}
	if l.Trigger != "" {
		fmt.Fprintf(b, "    trigger: %s\n", yamlString(l.Trigger))
	}
	if l.WorkoutStepIndex >= 0 {
		fmt.Fprintf(b, "    workout_step: %d\n", l.WorkoutStepIndex)
	}
	if !l.StartLocal.IsZero() {
		fmt.Fprintf(b, "    start_local: %s\n", l.StartLocal.In(loc).Format(time.RFC3339))
	}
	if l.Duration > 0 {
		fmt.Fprintf(b, "    duration_s: %d\n", int(l.Duration.Seconds()+0.5))
	}
	if l.ElapsedTime > 0 && l.ElapsedTime != l.Duration {
		fmt.Fprintf(b, "    elapsed_s: %d\n", int(l.ElapsedTime.Seconds()+0.5))
	}
	if l.Distance > 0 {
		fmt.Fprintf(b, "    distance_m: %s\n", formatFloat(l.Distance, 1))
	}
	if l.AvgHR > 0 {
		fmt.Fprintf(b, "    avg_hr: %d\n", l.AvgHR)
	}
	if l.MaxHR > 0 {
		fmt.Fprintf(b, "    max_hr: %d\n", l.MaxHR)
	}
	if l.AvgPower > 0 {
		fmt.Fprintf(b, "    avg_power: %d\n", l.AvgPower)
	}
	if l.AvgCadence > 0 {
		fmt.Fprintf(b, "    avg_cadence: %d\n", l.AvgCadence)
	}
	if l.AvgSpeed > 0 {
		fmt.Fprintf(b, "    avg_speed_mps: %s\n", formatFloat(l.AvgSpeed, 3))
	}
	if l.AvgPaceSecPerKm > 0 {
		fmt.Fprintf(b, "    avg_pace_sec_per_km: %d\n", l.AvgPaceSecPerKm)
	}
	// Elevation: prefer FIT lap message values (ground truth); fall back to
	// record-stream computation via applyElevation when FIT values are zero.
	var elevGain, elevLoss float64
	if l.ElevationGain > 0 || l.ElevationLoss > 0 {
		elevGain = l.ElevationGain
		elevLoss = l.ElevationLoss
	} else if len(records) > 0 && l.Distance > 0 {
		// Find lap start distance: first record at or after l.StartLocal.
		var lapStartDist float64
		for _, r := range records {
			if !r.Timestamp.Before(l.StartLocal) {
				lapStartDist = r.Distance
				break
			}
		}
		lapEndDist := lapStartDist + l.Distance
		// Collect records within this lap.
		var lapRecs []fitparse.Record
		for _, r := range records {
			if r.Distance >= lapStartDist && r.Distance <= lapEndDist {
				lapRecs = append(lapRecs, r)
			}
		}
		if len(lapRecs) > 0 {
				segs := []autoSplitSegment{{segment: 1, distanceM: l.Distance}}
				applyElevation(segs, lapRecs, lapStartDist, l.Distance, lapEndDist, 0, hasBarometer)
				elevGain = segs[0].elevationGainM
				elevLoss = segs[0].elevationLossM
			}
	}
	if elevGain > 0 {
		fmt.Fprintf(b, "    elevation_gain_m: %s\n", formatFloat(elevGain, 1))
	}
	if elevLoss > 0 {
		fmt.Fprintf(b, "    elevation_loss_m: %s\n", formatFloat(elevLoss, 1))
	}
	if l.Calories > 0 {
		fmt.Fprintf(b, "    calories: %d\n", l.Calories)
	}
	// Lap-level wind stats derived from GPS bearing vs activity wind direction.
	if windDeg != nil && l.Distance > 0 {
		var lapStartDist float64
		for _, r := range records {
			if !r.Timestamp.Before(l.StartLocal) {
				lapStartDist = r.Distance
				break
			}
		}
		hw, tw := segmentWindPct(records, lapStartDist, lapStartDist+l.Distance, *windDeg)
		fmt.Fprintf(b, "    headwind_pct: %s\n", formatFloat(hw, 1))
		fmt.Fprintf(b, "    tailwind_pct: %s\n", formatFloat(tw, 1))
	}
	// Auto-splits: divide long unsegmented active laps into equal segments.
	if autoSplitM > 0 && l.Distance > float64(autoSplitM) {
		segs := autoSplitLap(l, autoSplitM, records, hasBarometer)
		if len(segs) > 1 {
			b.WriteString("    auto_splits:\n")
			for _, s := range segs {
				fmt.Fprintf(b, "      - segment: %d\n", s.segment)
				fmt.Fprintf(b, "        source: auto_split\n")
				fmt.Fprintf(b, "        distance_m: %s\n", formatFloat(s.distanceM, 1))
				if s.durationS > 0 {
					fmt.Fprintf(b, "        duration_s: %d\n", s.durationS)
				}
				if s.avgPaceSecPerKm > 0 {
					fmt.Fprintf(b, "        avg_pace_sec_per_km: %d\n", s.avgPaceSecPerKm)
				}
				if s.avgHR > 0 {
					fmt.Fprintf(b, "        avg_hr: %d\n", s.avgHR)
				}
				if s.maxHR > 0 {
					fmt.Fprintf(b, "        max_hr: %d\n", s.maxHR)
				}
				if s.avgCadence > 0 {
					fmt.Fprintf(b, "        avg_cadence: %d\n", s.avgCadence)
				}
				if s.elevationGainM > 0 {
					fmt.Fprintf(b, "        elevation_gain_m: %s\n", formatFloat(s.elevationGainM, 1))
				}
				if s.elevationLossM > 0 {
					fmt.Fprintf(b, "        elevation_loss_m: %s\n", formatFloat(s.elevationLossM, 1))
				}
				// Per-segment wind stats from GPS bearing.
				if windDeg != nil {
					segStart := s.segStartDist
					hw, tw := segmentWindPct(records, segStart, segStart+s.distanceM, *windDeg)
					fmt.Fprintf(b, "        headwind_pct: %s\n", formatFloat(hw, 1))
					fmt.Fprintf(b, "        tailwind_pct: %s\n", formatFloat(tw, 1))
				}
			}
		}
	}
}

func writeInterval(b *bytes.Buffer, iv fitparse.Interval) {
	fmt.Fprintf(b, "  - kind: %s\n", yamlString(iv.Kind))
	if iv.WorkoutStepIndex >= 0 {
		fmt.Fprintf(b, "    workout_step: %d\n", iv.WorkoutStepIndex)
	}
	if len(iv.LapIndices) > 0 {
		ints := make([]string, len(iv.LapIndices))
		for i, n := range iv.LapIndices {
			ints[i] = fmt.Sprintf("%d", n)
		}
		fmt.Fprintf(b, "    laps: [%s]\n", strings.Join(ints, ", "))
	}
}

// autoSplitSegment holds the derived stats for one implicit split segment.
type autoSplitSegment struct {
	segment         int
	segStartDist    float64 // cumulative distance at segment start (metres)
	distanceM       float64
	durationS       int
	avgPaceSecPerKm int
	avgHR           int
	maxHR           int
	avgCadence      int
	elevationGainM  float64
	elevationLossM  float64
}

// autoSplitLap divides a single lap into segments of splitM metres using the
// per-second FIT record stream. Each segment gets real averages computed from
// the records whose cumulative distance falls within the segment's range.
// When records are absent or insufficient, falls back to proportional
// approximation from the lap summary.
// Only laps with intensity "active" are split; all others return nil.
func autoSplitLap(l fitparse.Lap, splitM int, records []fitparse.Record, hasBarometer bool) []autoSplitSegment {
	if l.Intensity != "active" {
		return nil
	}
	if l.Distance <= 0 || splitM <= 0 {
		return nil
	}
	n := int(l.Distance / float64(splitM))
	remainder := l.Distance - float64(n)*float64(splitM)
	total := n
	if remainder > 0.5 {
		total++
	}
	if total < 2 {
		return nil
	}

	// Find the lap's distance offset: the cumulative distance at the start of
	// this lap. We locate it by finding the first record at or after the lap's
	// start time.
	var lapStartDist float64
	lapStartDistSet := false
	if len(records) > 0 && !l.StartLocal.IsZero() {
		for _, r := range records {
			if !r.Timestamp.Before(l.StartLocal) {
				lapStartDist = r.Distance
				lapStartDistSet = true
				break
			}
		}
	}

	// Filter to records belonging to this lap by distance range.
	lapEndDist := lapStartDist + l.Distance
	var lapRecs []fitparse.Record
	if lapStartDistSet && len(records) > 0 {
		for _, r := range records {
			if r.Distance >= lapStartDist && r.Distance <= lapEndDist {
				lapRecs = append(lapRecs, r)
			}
		}
	}

	segs := make([]autoSplitSegment, total)
	for i := 0; i < total; i++ {
		segStart := lapStartDist + float64(i*splitM)
		segEnd := segStart + float64(splitM)
		if i == n {
			segEnd = lapEndDist
		}
		dist := segEnd - segStart

		var seg autoSplitSegment
		seg.segment = i + 1
		seg.segStartDist = segStart
		seg.distanceM = dist

		if len(lapRecs) > 0 {
			seg = segStatsFromRecords(lapRecs, i+1, segStart, segEnd, dist)
			seg.segStartDist = segStart
		} else {
			// Fallback: proportional from lap summary
			frac := dist / l.Distance
			durS := int(l.Duration.Seconds()*frac + 0.5)
			pace := 0
			if dist > 0 && durS > 0 {
				pace = int(float64(durS)/(dist/1000.0) + 0.5)
			}
			seg = autoSplitSegment{
				segment:         i + 1,
				segStartDist:    segStart,
				distanceM:       dist,
				durationS:       durS,
				avgPaceSecPerKm: pace,
				avgHR:           l.AvgHR,
				maxHR:           l.MaxHR,
				avgCadence:      l.AvgCadence,
			}
		}
		segs[i] = seg
	}

	// Elevation: run EWMA smoothing over the entire lap's altitude stream,
	// then apply hysteresis on the smoothed values per segment.
	// See docs/elevation-algorithm.md for rationale.
	if len(lapRecs) > 0 {
		applyElevation(segs, lapRecs, lapStartDist, float64(splitM), lapEndDist, n, hasBarometer)
	}

	return segs
}

// elevThresholds returns the hysteresis threshold in metres based on whether
// the altitude data is barometric (more precise) or GPS-only (noisier).
func elevThreshold(barometric bool) float64 {
	if barometric {
		return 2.0 // metres; matches Strava's barometric threshold
	}
	return 8.0 // metres; conservative for GPS-only altitude
}

// applyElevation computes per-segment elevation gain/loss using a two-step
// algorithm: EWMA smoothing over the full lap, then per-segment hysteresis.
// It writes directly into segs[].elevationGainM and segs[].elevationLossM.
func applyElevation(segs []autoSplitSegment, lapRecs []fitparse.Record, lapStartDist, splitM, lapEndDist float64, n int, barometric bool) {
	const ewmaAlpha = 0.1 // smoothing factor; lower = more smoothing

	thresh := elevThreshold(barometric)

	// Pass 1: EWMA smooth across the entire lap altitude stream.
	// We keep a parallel slice of (distance, smoothedAlt) for bucketing.
	type altPoint struct {
		dist float64
		alt  float64
	}
	var smoothed []altPoint
	var ewma float64
	ewmaInit := false
	for _, r := range lapRecs {
		if !r.AltitudeValid {
			continue
		}
		if !ewmaInit {
			ewma = r.Altitude
			ewmaInit = true
		} else {
			ewma = ewmaAlpha*r.Altitude + (1-ewmaAlpha)*ewma
		}
		smoothed = append(smoothed, altPoint{dist: r.Distance, alt: ewma})
	}
	if len(smoothed) < 2 {
		return
	}

	// Pass 2: for each segment, apply hysteresis on its slice of smoothed values.
	for i := range segs {
		segStart := lapStartDist + float64(i)*splitM
		segEnd := segStart + splitM
		if i == n {
			segEnd = lapEndDist
		}

		var gain, loss float64
		var committed float64
		committedSet := false
		for _, p := range smoothed {
			if p.dist < segStart || p.dist > segEnd {
				continue
			}
			if !committedSet {
				committed = p.alt
				committedSet = true
				continue
			}
			delta := p.alt - committed
			if delta >= thresh {
				gain += delta
				committed = p.alt
			} else if delta <= -thresh {
				loss -= delta
				committed = p.alt
			}
		}
		segs[i].elevationGainM = gain
		segs[i].elevationLossM = loss
	}
}

// segStatsFromRecords computes per-segment averages from the subset of records
// whose distance falls within [segStart, segEnd).
// Elevation is handled by the caller (autoSplitLap) using the lap's own
// Garmin-filtered TotalAscent/TotalDescent values distributed proportionally.
func segStatsFromRecords(recs []fitparse.Record, segment int, segStart, segEnd, dist float64) autoSplitSegment {
	var hrSum, hrCount, maxHR, cadSum, cadCount int
	var speedSum float64
	speedCount := 0
	var firstTime, lastTime time.Time

	for _, r := range recs {
		if r.Distance < segStart || r.Distance > segEnd {
			continue
		}
		if r.HR > 0 {
			hrSum += r.HR
			hrCount++
			if r.HR > maxHR {
				maxHR = r.HR
			}
		}
		if r.Cadence > 0 {
			cadSum += r.Cadence
			cadCount++
		}
		if r.Speed > 0 {
			speedSum += r.Speed
			speedCount++
		}
		if firstTime.IsZero() {
			firstTime = r.Timestamp
		}
		lastTime = r.Timestamp
	}

	seg := autoSplitSegment{
		segment:   segment,
		distanceM: dist,
	}
	if !firstTime.IsZero() && !lastTime.IsZero() {
		seg.durationS = int(lastTime.Sub(firstTime).Seconds() + 0.5)
		if seg.durationS < 1 {
			seg.durationS = 1
		}
	}
	if seg.durationS > 0 && dist > 0 {
		seg.avgPaceSecPerKm = int(float64(seg.durationS)/(dist/1000.0) + 0.5)
	} else if speedCount > 0 {
		avgSpeed := speedSum / float64(speedCount)
		if avgSpeed > 0 {
			seg.avgPaceSecPerKm = int(1000.0/avgSpeed + 0.5)
		}
	}
	if hrCount > 0 {
		seg.avgHR = hrSum / hrCount
		seg.maxHR = maxHR
	}
	if cadCount > 0 {
		seg.avgCadence = cadSum / cadCount
	}
	return seg
}

// writeWeather emits the weather: block for activities where has_weather is true.
func writeWeather(b *bytes.Buffer, s icu.ActivitySummary) {
	b.WriteString("weather:\n")
	fmt.Fprintf(b, "  temp_c: %s\n", formatFloat(s.AvgWeatherTemp, 1))
	fmt.Fprintf(b, "  feels_like_c: %s\n", formatFloat(s.AvgFeelsLike, 1))
	fmt.Fprintf(b, "  condition: %s\n", weatherCondition(s.MaxRain, s.AvgClouds))
	fmt.Fprintf(b, "  cloud_pct: %s\n", formatFloat(s.AvgClouds, 1))
	fmt.Fprintf(b, "  rain_mm: %s\n", formatFloat(s.MaxRain, 1))
	fmt.Fprintf(b, "  wind_speed_ms: %s\n", formatFloat(s.AvgWindSpeed, 1))
	fmt.Fprintf(b, "  wind_gust_ms: %s\n", formatFloat(s.AvgWindGust, 1))
	if s.PrevailingWindDeg != nil {
		fmt.Fprintf(b, "  wind_dir_deg: %d\n", *s.PrevailingWindDeg)
		fmt.Fprintf(b, "  wind_dir: %s\n", windDirection(*s.PrevailingWindDeg))
	}
	fmt.Fprintf(b, "  headwind_pct: %s\n", formatFloat(s.HeadwindPct, 1))
	fmt.Fprintf(b, "  tailwind_pct: %s\n", formatFloat(s.TailwindPct, 1))
}

// weatherCondition derives a human-readable condition string from cloud cover and rain.
func weatherCondition(rainMM, cloudPct float64) string {
	if rainMM >= 5 {
		return "Heavy Rain"
	}
	if rainMM > 0 {
		return "Rain"
	}
	if cloudPct >= 90 {
		return "Overcast"
	}
	if cloudPct >= 50 {
		return "Cloudy"
	}
	if cloudPct >= 10 {
		return "Partly Cloudy"
	}
	return "Clear"
}

// windDirection converts a bearing in degrees to a 16-point compass label.
func windDirection(deg int) string {
	dirs := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	// Each sector is 22.5°; offset by half a sector so N is centred on 0°.
	idx := int((float64(deg)+11.25)/22.5) % 16
	return dirs[idx]
}

// segmentWindPct computes headwind and tailwind percentages for a segment
// defined by [segStart, segEnd) metres of cumulative distance.
// windFromDeg is the direction the wind is coming FROM (meteorological convention).
// Returns (headwindPct, tailwindPct) in the range [0, 100].
// Returns (0, 0) when GPS data is unavailable or insufficient.
func segmentWindPct(records []fitparse.Record, segStart, segEnd float64, windFromDeg int) (headwind, tailwind float64) {
	// Collect GPS points within this segment.
	type pt struct{ lat, lon, dist float64 }
	var pts []pt
	for _, r := range records {
		if !r.LatLonValid {
			continue
		}
		if r.Distance >= segStart && r.Distance <= segEnd {
			pts = append(pts, pt{r.Lat, r.Lon, r.Distance})
		}
	}
	if len(pts) < 2 {
		return 0, 0
	}

	// Wind is FROM windFromDeg; the wind vector points TO windFromDeg+180.
	windToDeg := math.Mod(float64(windFromDeg+180), 360)

	// Compute distance-weighted average wind component across consecutive GPS
	// pairs. Each pair contributes cos(bearing - wind_to_deg) weighted by the
	// distance between the two points, so 200m of headwind and 800m of tailwind
	// produce a net result proportional to actual exposure rather than a mean
	// bearing that can mask direction reversals.
	var headwindSum, tailwindSum, totalDist float64
	for i := 1; i < len(pts); i++ {
		bearing := gpsBearing(pts[i-1].lat, pts[i-1].lon, pts[i].lat, pts[i].lon)
		pairDist := pts[i].dist - pts[i-1].dist
		if pairDist <= 0 {
			pairDist = haversineM(pts[i-1].lat, pts[i-1].lon, pts[i].lat, pts[i].lon)
		}
		angle := (bearing - windToDeg) * math.Pi / 180
		component := math.Cos(angle) // +1=tailwind, -1=headwind
		if component > 0 {
			tailwindSum += component * pairDist
		} else {
			headwindSum += -component * pairDist
		}
		totalDist += pairDist
	}
	if totalDist == 0 {
		return 0, 0
	}
	headwind = math.Round(headwindSum/totalDist*100*10) / 10
	tailwind = math.Round(tailwindSum/totalDist*100*10) / 10
	return headwind, tailwind
}

// gpsBearing returns the initial bearing in degrees [0, 360) from (lat1, lon1)
// to (lat2, lon2) using the forward azimuth formula.
func gpsBearing(lat1, lon1, lat2, lon2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δλ := (lon2 - lon1) * math.Pi / 180
	y := math.Sin(Δλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(Δλ)
	θ := math.Atan2(y, x) * 180 / math.Pi
	return math.Mod(θ+360, 360)
}

// haversineM returns the great-circle distance in metres between two lat/lon points.
func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000.0
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δφ := (lat2 - lat1) * math.Pi / 180
	Δλ := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) + math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
