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
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		lineNo := i + 1
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if isSkippableLine(trimmed) {
			continue
		}
		// Block-style repeat header: a line ending with "Nx" (optionally with label prefix
		// and/or trailing "-- note"), with no parenthesised body.
		if reps, label, note, ok := tryBlockRepeatHeader(trimmed); ok {
			step, consumed, err := parseBlockRepeat(lines, i, reps, label, note, lineNo)
			if err != nil {
				return nil, err
			}
			w.Steps = append(w.Steps, *step)
			i += consumed - 1 // for-loop will i++
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

// isSkippableLine returns true for blank lines and markdown formatting lines.
func isSkippableLine(trimmed string) bool {
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "#") {
		return true
	}
	if strings.HasPrefix(trimmed, "---") {
		return true
	}
	if strings.HasPrefix(trimmed, "**") {
		return true
	}
	if strings.HasPrefix(trimmed, "|") {
		return true
	}
	return false
}

// tryBlockRepeatHeader recognises a "Nx" line (with optional label prefix and "-- note").
// Returns (reps, label, note, true) on match; otherwise (0,"","",false).
func tryBlockRepeatHeader(trimmed string) (int, string, string, bool) {
	body, note := splitNote(trimmed)
	body = strings.TrimSpace(body)
	if !strings.HasSuffix(body, "x") && !strings.HasSuffix(body, "X") {
		return 0, "", "", false
	}
	// Find the last space before "x" to support "Label Nx"
	xIdx := len(body) - 1
	// Find where the number starts (scan backward from 'x')
	numEnd := xIdx
	numStart := numEnd
	for numStart > 0 && body[numStart-1] >= '0' && body[numStart-1] <= '9' {
		numStart--
	}
	if numStart == numEnd {
		return 0, "", "", false
	}
	numStr := body[numStart:numEnd]
	n, err := strconv.Atoi(numStr)
	if err != nil || n < 1 {
		return 0, "", "", false
	}
	label := strings.TrimSpace(body[:numStart])
	return n, label, note, true
}

// parseBlockRepeat consumes the header line at lines[start] plus the
// following contiguous "- step" lines (terminated by a blank line, EOF,
// or any non-step line). Returns the assembled Step and the number of
// source lines consumed (header + body lines).
func parseBlockRepeat(lines []string, start, reps int, label, note string, headerLineNo int) (*Step, int, error) {
	steps := []SimpleStep{}
	consumed := 1
	for j := start + 1; j < len(lines); j++ {
		raw := lines[j]
		lineNo := j + 1
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			consumed++ // include the blank line in the consumed range
			break
		}
		if strings.HasPrefix(trimmed, "#") {
			consumed++
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			break
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if body == "" {
			return nil, 0, &ParseError{Line: lineNo, Col: 1, Msg: "empty step in repeat block"}
		}
		body, sNote := splitNote(body)
		simple, err := parseSimple(body, lineNo)
		if err != nil {
			return nil, 0, err
		}
		simple.Note = sNote
		steps = append(steps, *simple)
		consumed++
	}
	if len(steps) < 2 {
		return nil, 0, &ParseError{Line: headerLineNo, Col: 1, Msg: fmt.Sprintf("repeat block %dx must contain at least 2 steps", reps)}
	}
	return &Step{
		Line:   headerLineNo,
		Repeat: &RepeatStep{Label: label, Reps: reps, Steps: steps, Note: note},
		Note:   note,
	}, consumed, nil
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
	// ramp: "<duration> ramp <zone>-<zone>" or "<duration> ramp <pct>%-<pct>%" or "<duration> ramp <pct>%-<pct>% Pace"
	if ramp, ok, err := tryParseRamp(body, lineNo); ok {
		if err != nil {
			return nil, err
		}
		return &Step{Ramp: ramp}, nil
	}
	// simple step (possibly with label, multi-token intensity)
	simple, err := parseSimple(body, lineNo)
	if err != nil {
		return nil, err
	}
	return &Step{Simple: simple}, nil
}

// tryParseRamp attempts to parse ramp step bodies.
func tryParseRamp(body string, lineNo int) (*RampStep, bool, error) {
	fields := strings.Fields(body)
	// Minimum: "<dur> ramp <range>"
	if len(fields) < 3 {
		return nil, false, nil
	}
	if !strings.EqualFold(fields[1], "ramp") {
		return nil, false, nil
	}
	dur, err := parseDuration(fields[0], lineNo)
	if err != nil {
		return nil, true, err
	}
	// Remaining tokens after "ramp" form the range
	rangePart := strings.Join(fields[2:], " ")
	// Try "Zx-Zy" zone range
	if strings.HasPrefix(rangePart, "Z") || strings.HasPrefix(rangePart, "z") {
		from, to, err := parseZoneRange(rangePart, lineNo)
		if err != nil {
			return nil, true, err
		}
		return &RampStep{Duration: dur, From: from, To: to, RampType: "zone"}, true, nil
	}
	// Try "N%-M% Pace" or "N%-M%"
	isPace := strings.HasSuffix(strings.ToLower(rangePart), " pace")
	pctPart := rangePart
	if isPace {
		pctPart = strings.TrimSpace(rangePart[:len(rangePart)-5])
	}
	if strings.Contains(pctPart, "-") && strings.HasSuffix(pctPart, "%") {
		dashIdx := strings.LastIndex(pctPart, "-")
		fromStr := strings.TrimSuffix(pctPart[:dashIdx], "%")
		toStr := strings.TrimSuffix(pctPart[dashIdx+1:], "%")
		from, err1 := strconv.Atoi(fromStr)
		to, err2 := strconv.Atoi(toStr)
		if err1 == nil && err2 == nil {
			rampType := "percent"
			if isPace {
				rampType = "pace_percent"
			}
			return &RampStep{Duration: dur, FromPercent: from, ToPercent: to, RampType: rampType}, true, nil
		}
	}
	return nil, true, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid ramp range %q", rangePart)}
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
	parts := strings.SplitN(inner, " / ", 2)
	if len(parts) != 2 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: "repeat body must be 'work / rest' (space around '/')"}, true
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
	return &RepeatStep{Reps: reps, Steps: []SimpleStep{*work, *restStep}}, nil, true
}

// parseSimple parses a step body that is a SimpleStep.
// Body may contain a label prefix, an amount, a multi-token intensity, and an optional cadence.
func parseSimple(body string, lineNo int) (*SimpleStep, error) {
	tokens := strings.Fields(body)
	if len(tokens) == 0 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: "empty step"}
	}

	// Find the index of the first amount token (duration or distance).
	amtIdx := -1
	for i, tok := range tokens {
		if looksLikeAmount(tok) {
			amtIdx = i
			break
		}
	}
	if amtIdx < 0 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("no amount (duration/distance) found in %q", body)}
	}

	// Everything before amtIdx is the label.
	label := strings.Join(tokens[:amtIdx], " ")

	// Parse the amount.
	amt, err := parseAmount(tokens[amtIdx], lineNo)
	if err != nil {
		return nil, err
	}

	// Remaining tokens after amount form intensity (and optional cadence).
	rest := tokens[amtIdx+1:]
	if len(rest) == 0 {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("missing intensity in %q", body)}
	}

	// Check for trailing cadence "Nrpm" or "N-Mrpm".
	var cadence *CadenceRange
	if len(rest) > 0 && looksLikeCadence(rest[len(rest)-1]) {
		cadence, err = parseCadence(rest[len(rest)-1], lineNo)
		if err != nil {
			return nil, err
		}
		rest = rest[:len(rest)-1]
	}

	// Parse multi-token intensity.
	intensity, err := parseIntensityTokens(rest, lineNo)
	if err != nil {
		return nil, err
	}

	return &SimpleStep{Label: label, Amount: amt, Intensity: intensity, Cadence: cadence}, nil
}

// looksLikeAmount returns true if tok could be a duration or distance amount token.
func looksLikeAmount(tok string) bool {
	if tok == "" {
		return false
	}
	lower := strings.ToLower(tok)
	// Known distance suffixes
	for _, sfx := range []string{"km", "mtr", "mi", "y"} {
		if strings.HasSuffix(lower, sfx) {
			prefix := tok[:len(tok)-len(sfx)]
			if prefix == "" {
				continue
			}
			// Accept integer or float prefix
			if _, err := strconv.Atoi(prefix); err == nil {
				return true
			}
			if _, err := strconv.ParseFloat(prefix, 64); err == nil {
				return true
			}
		}
	}
	// Check for duration characters: h, m, s
	if strings.ContainsAny(lower, "hms") {
		// Must start with a digit
		if len(tok) > 0 && tok[0] >= '0' && tok[0] <= '9' {
			return true
		}
	}
	// Bare "Nm" where N >= 50 is metres distance
	if strings.HasSuffix(lower, "m") && !strings.ContainsAny(lower[:len(lower)-1], "hms") {
		numStr := tok[:len(tok)-1]
		if n, err := strconv.Atoi(numStr); err == nil && n >= 50 {
			return true
		}
	}
	return false
}

// looksLikeCadence returns true if tok looks like "Nrpm" or "N-Mrpm".
func looksLikeCadence(tok string) bool {
	return strings.HasSuffix(strings.ToLower(tok), "rpm")
}

// parseCadence parses "90rpm" or "90-100rpm".
func parseCadence(tok string, lineNo int) (*CadenceRange, error) {
	s := strings.ToLower(tok)
	s = strings.TrimSuffix(s, "rpm")
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		a, err1 := strconv.Atoi(parts[0])
		b, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid cadence %q", tok)}
		}
		return &CadenceRange{RPM: a, RPMTo: b}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid cadence %q", tok)}
	}
	return &CadenceRange{RPM: n}, nil
}

func parseAmount(tok string, lineNo int) (Amount, error) {
	if tok == "" {
		return Amount{}, &ParseError{Line: lineNo, Col: 1, Msg: "empty amount"}
	}
	lower := strings.ToLower(tok)
	// "mtr" suffix = metres
	if strings.HasSuffix(lower, "mtr") {
		numStr := tok[:len(tok)-3]
		n, err := strconv.Atoi(numStr)
		if err != nil {
			return Amount{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid distance %q: %v", tok, err)}
		}
		return Amount{Distance: &Distance{Value: n, Unit: "m", Raw: fmt.Sprintf("%dm", n)}}, nil
	}
	// "km" suffix — support decimal like 3.2km
	if strings.HasSuffix(lower, "km") {
		numStr := tok[:len(tok)-2]
		if n, err := strconv.Atoi(numStr); err == nil {
			return Amount{Distance: &Distance{Value: n, Unit: "km", Raw: tok}}, nil
		}
		// Decimal km: store as metres with unit "km_frac" to preserve Raw rendering
		if f, err := strconv.ParseFloat(numStr, 64); err == nil && f > 0 {
			return Amount{Distance: &Distance{Value: int(f * 1000), Unit: "km_frac", Raw: tok}}, nil
		}
		return Amount{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid distance %q", tok)}
	}
	// "mi" distance suffix (not to be confused with "mi" pace unit)
	// We only treat "Nmi" as distance if it doesn't contain colons (pace would have colons).
	if strings.HasSuffix(lower, "mi") && !strings.Contains(tok, ":") {
		n, err := strconv.Atoi(tok[:len(tok)-2])
		if err == nil && n > 0 {
			return Amount{Distance: &Distance{Value: n, Unit: "mi", Raw: tok}}, nil
		}
	}
	// "y" suffix
	if strings.HasSuffix(lower, "y") {
		n, err := strconv.Atoi(tok[:len(tok)-1])
		if err != nil {
			return Amount{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid distance %q: %v", tok, err)}
		}
		return Amount{Distance: &Distance{Value: n, Unit: "y", Raw: tok}}, nil
	}
	// "m" suffix — distance if N >= 50, else duration minutes
	if strings.HasSuffix(lower, "m") && !strings.ContainsAny(lower, "hs") {
		numStr := tok[:len(tok)-1]
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
	"open":      true, // lap-button terminated; device ignores duration
	"freeride":  true, // ERG off / no target; device ignores duration
}

// parseIntensityTokens parses one or more tokens as an intensity.
// This handles multi-token intensities like "Z2 HR", "Z2 Pace", "70% HR", "4:55-4:35 Pace", etc.
func parseIntensityTokens(tokens []string, lineNo int) (Intensity, error) {
	if len(tokens) == 0 {
		return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: "empty intensity"}
	}

	// Join all tokens for pattern matching
	joined := strings.Join(tokens, " ")

	// "intensity=recovery"
	if strings.ToLower(joined) == "intensity=recovery" {
		return Intensity{IsRecovery: true}, nil
	}

	// Two-token intensities: "Z2 HR", "Z2-Z3 HR", "70% HR", "70-80% HR", "95% LTHR",
	// "Z2 Pace", "Z1-Z2 Pace", "78-82% Pace"
	if len(tokens) == 2 {
		qualifier := strings.ToUpper(tokens[1])

		switch qualifier {
		case "HR", "LTHR":
			isLTHR := qualifier == "LTHR"
			hr, err := parseHRTarget(tokens[0], isLTHR, lineNo)
			if err != nil {
				return Intensity{}, err
			}
			return Intensity{HR: hr}, nil

		case "PACE":
			// Could be "Z2 Pace", "Z1-Z2 Pace", "78-82% Pace", or "4:00 Pace", "4:55-4:35 Pace"
			tok0 := tokens[0]
			// Zone-based pace: "Z2 Pace" or "Z1-Z2 Pace"
			if tok0[0] == 'Z' || tok0[0] == 'z' {
				pz, err := parsePaceZone(tok0, lineNo)
				if err != nil {
					return Intensity{}, err
				}
				return Intensity{PaceZone: pz}, nil
			}
			// Percent pace: "78-82% Pace" or "78% Pace"
			if strings.HasSuffix(tok0, "%") {
				pp, err := parsePacePercent(tok0, lineNo)
				if err != nil {
					return Intensity{}, err
				}
				return Intensity{PacePercent: pp}, nil
			}
			// Absolute pace with "Pace" keyword: "4:00 Pace" or "4:55-4:35 Pace"
			p, err := parseAbsolutePaceToken(tok0, lineNo, true)
			if err != nil {
				return Intensity{}, err
			}
			return Intensity{Pace: p}, nil
		}
	}

	// Single-token intensities
	if len(tokens) == 1 {
		return parseIntensity(tokens[0], lineNo)
	}

	return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("unknown intensity %q", joined)}
}

// parseHRTarget parses the first token of a "TOKEN HR/LTHR" intensity.
func parseHRTarget(tok string, isLTHR bool, lineNo int) (*HRTarget, error) {
	// Zone range: "Z2-Z3"
	if (tok[0] == 'Z' || tok[0] == 'z') && strings.Contains(tok, "-") {
		parts := strings.SplitN(tok, "-", 2)
		z1, err := parseZone(parts[0], lineNo)
		if err != nil {
			return nil, err
		}
		z2, err := parseZone(parts[1], lineNo)
		if err != nil {
			return nil, err
		}
		return &HRTarget{Zone: &z1, ZoneTo: &z2, IsLTHR: isLTHR}, nil
	}
	// Single zone: "Z2"
	if tok[0] == 'Z' || tok[0] == 'z' {
		z, err := parseZone(tok, lineNo)
		if err != nil {
			return nil, err
		}
		return &HRTarget{Zone: &z, IsLTHR: isLTHR}, nil
	}
	// Percent range: "70-80%"
	if strings.HasSuffix(tok, "%") && strings.Contains(tok, "-") {
		inner := strings.TrimSuffix(tok, "%")
		dashIdx := strings.Index(inner, "-")
		a, err1 := strconv.Atoi(inner[:dashIdx])
		b, err2 := strconv.Atoi(inner[dashIdx+1:])
		if err1 != nil || err2 != nil {
			return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid HR percent range %q", tok)}
		}
		return &HRTarget{Percent: a, PercentTo: b, IsLTHR: isLTHR}, nil
	}
	// Single percent: "70%"
	if strings.HasSuffix(tok, "%") {
		n, err := strconv.Atoi(strings.TrimSuffix(tok, "%"))
		if err != nil {
			return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid HR percent %q", tok)}
		}
		return &HRTarget{Percent: n, IsLTHR: isLTHR}, nil
	}
	return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid HR target %q", tok)}
}

// parsePaceZone parses "Z2" or "Z1-Z2" as a PaceZoneTarget.
func parsePaceZone(tok string, lineNo int) (*PaceZoneTarget, error) {
	upper := strings.ToUpper(tok)
	if strings.Contains(upper, "-") {
		parts := strings.SplitN(upper, "-", 2)
		z1, err := parseZone(parts[0], lineNo)
		if err != nil {
			return nil, err
		}
		z2, err := parseZone(parts[1], lineNo)
		if err != nil {
			return nil, err
		}
		return &PaceZoneTarget{Zone: z1.N, ZoneTo: z2.N}, nil
	}
	z, err := parseZone(upper, lineNo)
	if err != nil {
		return nil, err
	}
	return &PaceZoneTarget{Zone: z.N}, nil
}

// parsePacePercent parses "78-82%" or "78%" as a PacePercentTarget.
func parsePacePercent(tok string, lineNo int) (*PacePercentTarget, error) {
	inner := strings.TrimSuffix(tok, "%")
	if strings.Contains(inner, "-") {
		parts := strings.SplitN(inner, "-", 2)
		a, err1 := strconv.Atoi(parts[0])
		b, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid pace percent range %q", tok)}
		}
		return &PacePercentTarget{Percent: a, PercentTo: b}, nil
	}
	n, err := strconv.Atoi(inner)
	if err != nil {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid pace percent %q", tok)}
	}
	return &PacePercentTarget{Percent: n}, nil
}

// parseAbsolutePaceToken parses "4:00" or "4:55-4:35" as a Pace target.
// isPaceWord indicates the "Pace" keyword was present (vs /km or /mi).
func parseAbsolutePaceToken(tok string, lineNo int, isPaceWord bool) (*Pace, error) {
	// Range: "4:55-4:35"
	if strings.Count(tok, ":") == 2 {
		// Format M:SS-M:SS
		dashIdx := strings.LastIndex(tok, "-")
		if dashIdx > 0 {
			p1 := tok[:dashIdx]
			p2 := tok[dashIdx+1:]
			secs1, ok1 := parsePaceMMSS(p1)
			secs2, ok2 := parsePaceMMSS(p2)
			if ok1 && ok2 {
				// p1 is the slow end, p2 is the fast end
				raw := fmt.Sprintf("%s-%s Pace", p1, p2)
				return &Pace{Seconds: secs2, SecondsEnd: secs1, Unit: "km", Raw: raw, IsPaceWord: true}, nil
			}
		}
	}
	// Single pace
	secs, ok := parsePaceMMSS(tok)
	if !ok {
		return nil, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid pace %q", tok)}
	}
	var raw string
	if isPaceWord {
		min := secs / 60
		sec := secs % 60
		raw = fmt.Sprintf("%d:%02d Pace", min, sec)
	} else {
		raw = tok + "/km"
	}
	return &Pace{Seconds: secs, Unit: "km", Raw: raw, IsPaceWord: isPaceWord}, nil
}

// parsePaceMMSS parses "M:SS" or "MM:SS" and returns total seconds.
func parsePaceMMSS(s string) (int, bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	min, err1 := strconv.Atoi(parts[0])
	sec, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	if min < 0 || sec < 0 || sec >= 60 {
		return 0, false
	}
	total := min*60 + sec
	if total <= 0 {
		return 0, false
	}
	return total, true
}

func parseIntensity(tok string, lineNo int) (Intensity, error) {
	if tok == "" {
		return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: "empty intensity"}
	}
	// "intensity=recovery"
	if strings.ToLower(tok) == "intensity=recovery" {
		return Intensity{IsRecovery: true}, nil
	}
	// Watts: "220w" or "200-240w"
	if strings.HasSuffix(strings.ToLower(tok), "w") && !strings.ContainsAny(tok, ":/%") {
		inner := tok[:len(tok)-1]
		if strings.Contains(inner, "-") {
			parts := strings.SplitN(inner, "-", 2)
			a, err1 := strconv.Atoi(parts[0])
			b, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				return Intensity{Watts: &WattsTarget{Watts: a, WattsTo: b}}, nil
			}
		} else {
			n, err := strconv.Atoi(inner)
			if err == nil {
				return Intensity{Watts: &WattsTarget{Watts: n}}, nil
			}
		}
	}
	// Custom zone: "CZ1" or "CZ2-CZ3"
	if strings.HasPrefix(strings.ToUpper(tok), "CZ") {
		upper := strings.ToUpper(tok)
		inner := upper[2:]
		if strings.Contains(inner, "-CZ") {
			parts := strings.SplitN(inner, "-CZ", 2)
			a, err1 := strconv.Atoi(parts[0])
			b, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				return Intensity{CustomZone: &CustomZoneTarget{Zone: a, ZoneTo: b}}, nil
			}
		} else if strings.Contains(inner, "-") {
			parts := strings.SplitN(inner, "-", 2)
			a, err1 := strconv.Atoi(parts[0])
			b, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				return Intensity{CustomZone: &CustomZoneTarget{Zone: a, ZoneTo: b}}, nil
			}
		} else {
			n, err := strconv.Atoi(inner)
			if err == nil {
				return Intensity{CustomZone: &CustomZoneTarget{Zone: n}}, nil
			}
		}
	}
	// FTP percent
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
	// Zone
	if (tok[0] == 'Z' || tok[0] == 'z') && len(tok) == 2 {
		n, err := strconv.Atoi(tok[1:])
		if err != nil || n < 1 || n > 6 {
			return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid zone %q (expected Z1..Z6)", tok)}
		}
		return Intensity{Zone: &Zone{N: n}}, nil
	}
	// Absolute pace with /km or /mi suffix, or bare M:SS
	if p, ok := tryParsePace(tok); ok {
		return Intensity{Pace: p}, nil
	}
	// Named intensity
	low := strings.ToLower(tok)
	if namedIntensities[low] {
		return Intensity{Named: low}, nil
	}
	return Intensity{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("unknown intensity %q", tok)}
}

// tryParsePace parses a pace token of the form "M:SS", "MM:SS",
// optionally suffixed with "/km" or "/mi" (default km).
// Examples: "3:55", "3:55/km", "4:30/mi", "10:00/mi".
func tryParsePace(tok string) (*Pace, bool) {
	unit := "km"
	s := tok
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		u := strings.ToLower(s[idx+1:])
		if u != "km" && u != "mi" {
			return nil, false
		}
		unit = u
		s = s[:idx]
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return nil, false
	}
	min, err1 := strconv.Atoi(parts[0])
	sec, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil, false
	}
	if min < 0 || sec < 0 || sec >= 60 {
		return nil, false
	}
	total := min*60 + sec
	if total <= 0 {
		return nil, false
	}
	// Canonical raw form always includes the unit.
	raw := fmt.Sprintf("%d:%02d/%s", min, sec, unit)
	return &Pace{Seconds: total, Unit: unit, Raw: raw}, true
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
	upper := strings.ToUpper(tok)
	if len(upper) != 2 || upper[0] != 'Z' {
		return Zone{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid zone %q (expected Zx)", tok)}
	}
	n, err := strconv.Atoi(upper[1:])
	if err != nil || n < 1 || n > 6 {
		return Zone{}, &ParseError{Line: lineNo, Col: 1, Msg: fmt.Sprintf("invalid zone %q (expected Z1..Z6)", tok)}
	}
	return Zone{N: n}, nil
}
