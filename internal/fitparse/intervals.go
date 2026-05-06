package fitparse

// GroupIntervals partitions laps into intervals.
//
// Two strategies, in order:
//
//  1. Workout-step grouping: when laps carry [Lap.WorkoutStepIndex]
//     values, contiguous laps with the same step index are grouped into
//     one [Interval]. Step transitions begin a new interval.
//  2. Heuristic grouping: contiguous laps that share an intensity are
//     coalesced. A repeating active/rest pattern (>=2 reps) is folded
//     into a single "work_set" interval.
//
// The function preserves lap order and produces a partition (every lap
// appears in exactly one interval). It returns nil for an empty input.
func GroupIntervals(laps []Lap) []Interval {
	if len(laps) == 0 {
		return nil
	}
	if anyHaveWorkoutStep(laps) {
		return groupByWorkoutStep(laps)
	}
	return groupHeuristic(laps)
}

func anyHaveWorkoutStep(laps []Lap) bool {
	for _, l := range laps {
		if l.WorkoutStepIndex >= 0 {
			return true
		}
	}
	return false
}

func groupByWorkoutStep(laps []Lap) []Interval {
	var out []Interval
	cur := Interval{WorkoutStepIndex: -2} // sentinel: no current group
	for _, l := range laps {
		if l.WorkoutStepIndex != cur.WorkoutStepIndex {
			if len(cur.LapIndices) > 0 {
				out = append(out, cur)
			}
			cur = Interval{
				Kind:             intervalKind(l.Intensity),
				WorkoutStepIndex: l.WorkoutStepIndex,
			}
		}
		cur.LapIndices = append(cur.LapIndices, l.Index)
	}
	if len(cur.LapIndices) > 0 {
		out = append(out, cur)
	}
	return out
}

func groupHeuristic(laps []Lap) []Interval {
	// First pass: coalesce contiguous laps with the same intensity.
	var runs []intensityRun
	for _, l := range laps {
		if len(runs) > 0 && runs[len(runs)-1].intensity == l.Intensity {
			runs[len(runs)-1].idx = append(runs[len(runs)-1].idx, l.Index)
			continue
		}
		runs = append(runs, intensityRun{intensity: l.Intensity, idx: []int{l.Index}})
	}

	// Second pass: detect repeated active/rest blocks and fold into
	// a single "work_set" interval. We look for a strictly alternating
	// {work, rest, work, rest, ...} pattern of at least 2 reps where
	// "work" is active|interval|other and "rest" is rest|recovery.
	var out []Interval
	i := 0
	for i < len(runs) {
		if reps := workSetLength(runs[i:]); reps >= 2 {
			ws := Interval{Kind: "work_set", WorkoutStepIndex: -1}
			for j := 0; j < reps*2; j++ {
				ws.LapIndices = append(ws.LapIndices, runs[i+j].idx...)
			}
			out = append(out, ws)
			i += reps * 2
			continue
		}
		r := runs[i]
		out = append(out, Interval{
			Kind:             intervalKind(r.intensity),
			WorkoutStepIndex: -1,
			LapIndices:       append([]int(nil), r.idx...),
		})
		i++
	}
	return out
}

// intensityRun is a helper for [groupHeuristic]: a contiguous slice of
// laps that share an intensity string.
type intensityRun struct {
	intensity string
	idx       []int
}

// workSetLength returns the number of work/rest repetitions starting
// at runs[0], or 0 if the head is not a work-rest pair. A run sequence
// {work, rest, work, rest} returns 2.
func workSetLength(runs []intensityRun) int {
	if len(runs) < 2 {
		return 0
	}
	if !isWork(runs[0].intensity) || !isRest(runs[1].intensity) {
		return 0
	}
	reps := 0
	for j := 0; j+1 < len(runs); j += 2 {
		if !isWork(runs[j].intensity) || !isRest(runs[j+1].intensity) {
			break
		}
		reps++
	}
	if reps < 2 {
		return 0
	}
	return reps
}

func isWork(intensity string) bool {
	switch intensity {
	case "active", "interval", "other":
		return true
	default:
		return false
	}
}

func isRest(intensity string) bool {
	switch intensity {
	case "rest", "recovery":
		return true
	default:
		return false
	}
}

// intervalKind maps a lap intensity to the [Interval.Kind] string used
// in the workspace YAML. Unknown intensities fall through to "other".
func intervalKind(intensity string) string {
	switch intensity {
	case "warmup", "cooldown", "rest", "recovery", "active", "interval":
		return intensity
	case "":
		return "other"
	default:
		return intensity
	}
}
