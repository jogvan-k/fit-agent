package workoutdsl

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ParseError is a parse error with line/column context.
type ParseError struct {
	Line int
	Col  int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("line %d:%d: %s", e.Line, e.Col, e.Msg)
}

// Parse parses the DSL source and returns a Workout. Empty source
// produces a Workout with zero steps and nil error.
func Parse(src string) (*Workout, error) {
	w := &Workout{}
	lines := strings.Split(src, "\n")
	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			return nil, &ParseError{Line: lineNo, Col: indexOfFirstNonSpace(line) + 1, Msg: `step must start with "- "`}
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if body == "" {
			return nil, &ParseError{Line: lineNo, Col: 1, Msg: "empty step"}
		}
		body, note := splitNote(body)
		step, err := parseStepBody(body, lineNo)
		if err != nil {
			return nil, err
		}
		step.Line = lineNo
		// Notes on the wrapping Step (and mirror onto the leaf for renderers).
		step.Note = note
		switch {
		case step.Simple != nil:
			step.Simple.Note = note
		case step.Repeat != nil:
			step.Repeat.Note = note
		case step.Ramp != nil:
			step.Ramp.Note = note
		}
		w.Steps = append(w.Steps, *step)
	}
	return w, nil
}

func indexOfFirstNonSpace(s string) int {
	for i, r := range s {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return len(s)
}

// splitNote returns the body before "--" and the trimmed note after it.
// If no "--" sequence is present, returns body unchanged and "".
// Only "--" sequences outside of parentheses are considered, so notes
// inside a repeat's work/rest body are left alone for the inner parser.
func splitNote(body string) (string, string) {
	depth := 0
	for i := 0; i+1 < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '-':
			if depth == 0 && body[i+1] == '-' {
				main := strings.TrimSpace(body[:i])
				note := strings.TrimSpace(body[i+2:])
				return main, note
			}
		}
	}
	return strings.TrimSpace(body), ""
}

// parseStepBody parses the body of a step line (without the leading "- "
// or trailing "-- note"). It returns a Step with exactly one of
// Simple/Repeat/Ramp set; the wrapping Step's Line and Note are filled
// by the caller.
func parseStepBody(body string, lineNo int) (*Step, error) {
	if body == "" {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: "empty step body"}
	}
	if rep, err, matched := tryParseRepeat(body, lineNo); matched {
		if err != nil {
			return nil, err
		}
		return &Step{Repeat: rep}, nil
	}
	fields := strings.Fields(body)
	// ramp: "<duration> ramp <zone>-<zone>"
	if len(fields) == 3 && strings.EqualFold(fields[1], "ramp") {
		dur, err := parseDuration(fields[0], lineNo)
		if err != nil {
			return nil, err
		}
		from, to, err := parseZoneRange(fields[2], lineNo)
		if err != nil {
			return nil, err
		}
		return &Step{Ramp: &RampStep{Duration: dur, From: from, To: to}}, nil
	}
	// simple: "<amount> <intensity>"
	if len(fields) != 2 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("expected '<amount> <intensity>', got %q", body)}
	}
	amt, err := parseAmount(fields[0], lineNo)
	if err != nil {
		return nil, err
	}
	intensity, err := parseIntensity(fields[1], lineNo)
	if err != nil {
		return nil, err
	}
	return &Step{Simple: &SimpleStep{Amount: amt, Intensity: intensity}}, nil
}

// tryParseRepeat looks for "Nx (work / rest)" and returns (repeat, err,
// matched). matched=false means the body didn't look like a repeat at
// all and the caller should try other shapes.
func tryParseRepeat(body string, lineNo int) (*RepeatStep, error, bool) {
	// Find "Nx" prefix.
	xIdx := strings.IndexByte(body, 'x')
	if xIdx <= 0 {
		return nil, nil, false
	}
	repsStr := body[:xIdx]
	if _, err := strconv.Atoi(repsStr); err != nil {
		return nil, nil, false
	}
	rest := strings.TrimSpace(body[xIdx+1:])
	if !strings.HasPrefix(rest, "(") {
		return nil, nil, false
	}
	end := strings.LastIndexByte(rest, ')')
	if end < 0 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: "missing closing ')' in repeat"}, true
	}
	inner := rest[1:end]
	parts := strings.SplitN(inner, "/", 2)
	if len(parts) != 2 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: "repeat body must be 'work / rest'"}, true
	}
	reps, _ := strconv.Atoi(repsStr)
	if reps < 1 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: "repeat count must be >= 1"}, true
	}
	workBody, workNote := splitNote(strings.TrimSpace(parts[0]))
	restBody, restNote := splitNote(strings.TrimSpace(parts[1]))
	work, err := parseSimple(workBody, lineNo)
	if err != nil {
		return nil, err, true
	}
	work.Note = workNote
	restStep, err := parseSimple(restBody, lineNo)
	if err != nil {
		return nil, err, true
	}
	restStep.Note = restNote
	return &RepeatStep{Reps: reps, Work: *work, Rest: *restStep}, nil, true
}

func parseSimple(body string, lineNo int) (*SimpleStep, error) {
	fields := strings.Fields(body)
	if len(fields) != 2 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("expected '<amount> <intensity>', got %q", body)}
	}
	amt, err := parseAmount(fields[0], lineNo)
	if err != nil {
		return nil, err
	}
	intensity, err := parseIntensity(fields[1], lineNo)
	if err != nil {
		return nil, err
	}
	return &SimpleStep{Amount: amt, Intensity: intensity}, nil
}

func parseAmount(tok string, lineNo int) (Amount, error) {
	if tok == "" {
		return Amount{}, &ParseError{Line: lineNo, Col: 1, Msg: "empty amount"}
	}
	// Distance suffixes first (longest match).
	switch {
	case strings.HasSuffix(tok, "km"):
		n, err := strconv.Atoi(strings.TrimSuffix(tok, "km"))
		if err != nil {
			return Amount{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid distance %q: %v", tok, err)}
		}
		return Amount{Distance: &Distance{Value: n, Unit: "km", Raw: tok}}, nil
	case strings.HasSuffix(tok, "y"):
		n, err := strconv.Atoi(strings.TrimSuffix(tok, "y"))
		if err != nil {
			return Amount{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid distance %q: %v", tok, err)}
		}
		return Amount{Distance: &Distance{Value: n, Unit: "y", Raw: tok}}, nil
	}
	// Could be either "400m" (distance) or a duration "5m"/"30s"/"1h30m".
	// Heuristic: a single "Nm" with N >= 50 is distance (50m..; durations
	// max practically 90m). To avoid heuristics, we make distance explicit
	// only via "km"/"y", and treat a bare "Nm" as MINUTES. Distance in
	// metres must be written "400m" — but that conflicts. Resolution:
	// only "km" and "y" are distance units inside a single token; metres
	// distances also use "m" but distinguished by trailing "m" being the
	// only character AND the number being >= 50 (typical track repeat).
	// To keep this deterministic and avoid surprises, we adopt:
	//   - "Nm" where N <= 6 and there's no "h"/"s" -> minutes
	//   - "Nm" where N > 6 -> minutes too
	// Distances in metres are not supported in the bare "m" form; users
	// should write "400m" — which the spec supports — so we use a
	// stricter rule: if tok ends with "m" and has no h/s/digit-suffix
	// quirks AND value >= 50, treat as distance. Otherwise duration.
	if strings.HasSuffix(tok, "m") && !strings.ContainsAny(tok, "hs") {
		numStr := strings.TrimSuffix(tok, "m")
		if n, err := strconv.Atoi(numStr); err == nil {
			if n >= 50 {
				return Amount{Distance: &Distance{Value: n, Unit: "m", Raw: tok}}, nil
			}
		}
	}
	dur, err := parseDuration(tok, lineNo)
	if err != nil {
		return Amount{}, err
	}
	return Amount{Duration: &dur}, nil
}

// parseDuration parses tokens like "30s", "5m", "1h", "1h30m", "2h15m30s".
func parseDuration(tok string, lineNo int) (Duration, error) {
	if tok == "" {
		return Duration{}, &ParseError{Line: lineNo, Col: 1, Msg: "empty duration"}
	}
	total := 0
	num := 0
	haveNum := false
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		switch {
		case c >= '0' && c <= '9':
			num = num*10 + int(c-'0')
			haveNum = true
		case c == 'h':
			if !haveNum {
				return Duration{}, &ParseError{Line: lineNo, Col: i + 1, Msg: "missing number before 'h'"}
			}
			total += num * 3600
			num, haveNum = 0, false
		case c == 'm':
			if !haveNum {
				return Duration{}, &ParseError{Line: lineNo, Col: i + 1, Msg: "missing number before 'm'"}
			}
			total += num * 60
			num, haveNum = 0, false
		case c == 's':
			if !haveNum {
				return Duration{}, &ParseError{Line: lineNo, Col: i + 1, Msg: "missing number before 's'"}
			}
			total += num
			num, haveNum = 0, false
		default:
			return Duration{}, &ParseError{Line: lineNo, Col: i + 1, Msg: fmt.Sprintf("unexpected %q in duration %q", c, tok)}
		}
	}
	if haveNum {
		return Duration{}, &ParseError{Line: lineNo, Col: len(tok), Msg: fmt.Sprintf("duration %q missing unit suffix", tok)}
	}
	if total <= 0 {
		return Duration{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("duration %q must be > 0", tok)}
	}
	return Duration{Seconds: total, Raw: tok}, nil
}

var namedIntensities = map[string]bool{
	"recovery":  true,
	"easy":      true,
	"tempo":     true,
	"threshold": true,
	"vo2":       true,
	"anaerobic": true,
	"sprint":    true,
}

func parseIntensity(tok string, lineNo int) (Intensity, error) {
	if tok == "" {
		return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: "empty intensity"}
	}
	if strings.HasSuffix(tok, "%") {
		n, err := strconv.Atoi(strings.TrimSuffix(tok, "%"))
		if err != nil {
			return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid percent %q: %v", tok, err)}
		}
		if n < 0 || n > 200 {
			return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("percent %d out of range 0..200", n)}
		}
		return Intensity{Percent: &n}, nil
	}
	if (tok[0] == 'Z' || tok[0] == 'z') && len(tok) == 2 {
		n, err := strconv.Atoi(tok[1:])
		if err != nil || n < 1 || n > 6 {
			return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid zone %q (expected Z1..Z6)", tok)}
		}
		return Intensity{Zone: &Zone{N: n}}, nil
	}
	low := strings.ToLower(tok)
	if namedIntensities[low] {
		return Intensity{Named: low}, nil
	}
	return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("unknown intensity %q", tok)}
}

func parseZoneRange(tok string, lineNo int) (Zone, Zone, error) {
	parts := strings.SplitN(tok, "-", 2)
	if len(parts) != 2 {
		return Zone{}, Zone{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("ramp range %q must be 'Zx-Zy'", tok)}
	}
	from, err := parseZone(parts[0], lineNo)
	if err != nil {
		return Zone{}, Zone{}, err
	}
	to, err := parseZone(parts[1], lineNo)
	if err != nil {
		return Zone{}, Zone{}, err
	}
	return from, to, nil
}

func parseZone(tok string, lineNo int) (Zone, error) {
	if len(tok) != 2 || (tok[0] != 'Z' && tok[0] != 'z') {
		return Zone{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid zone %q (expected Zx)", tok)}
	}
	n, err := strconv.Atoi(tok[1:])
	if err != nil || n < 1 || n > 6 {
		return Zone{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid zone %q (expected Z1..Z6)", tok)}
	}
	return Zone{N: n}, nil
}
