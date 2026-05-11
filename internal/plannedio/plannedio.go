// Package plannedio reads and writes the per-day planned-workout
// markdown files under fit-agent/planned-workouts/YYYY-MM-DD.md.
//
// The file format is documented in
// internal/templates/skills/workout-builder/SKILL.md and looks like:
//
//	---
//	fit-agent:
//	  kind: planned-workout-day
//	  date: 2026-05-04
//	workouts:
//	  - name: "Z2 Endurance"
//	    type: Ride
//	    moving_time_s: 4500
//	    icu_event_id: null
//	---
//
//	## Z2 Endurance
//
//	Easy aerobic ride.
//
//	```fit-workout
//	- 10m Z1
//	- 60m Z2
//	- 5m Z1
//	```
//
// Multiple workouts per day are allowed: more entries in the workouts:
// list and one "## name" section per workout. The name in frontmatter
// is the join key with the section heading.
package plannedio

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Day is one parsed planned-workouts/YYYY-MM-DD.md file.
type Day struct {
	Path     string      // source file path
	Date     string      // ISO date from frontmatter, e.g. "2026-05-04"
	Workouts []Workout   // one entry per top-level "## name" section
	Front    Frontmatter // raw frontmatter (kept for round-trip writes)
	Raw      string      // original file contents (for re-writing with id stamped in)
}

// Frontmatter is the parsed YAML at the top of the file.
type Frontmatter struct {
	FitAgent struct {
		Kind string `yaml:"kind"`
		Date string `yaml:"date"`
	} `yaml:"fit-agent"`
	Workouts []WorkoutMeta `yaml:"workouts"`
}

// WorkoutMeta is one entry from the frontmatter workouts: list.
type WorkoutMeta struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	MovingTimeS int    `yaml:"moving_time_s,omitempty"`
	IcuEventID  *int64 `yaml:"icu_event_id"` // pointer so we can distinguish null from 0
	// Description is a raw intervals.icu workout description used verbatim
	// when no fit-workout DSL block is present. DSL takes precedence.
	Description string `yaml:"description,omitempty"`
}

// Workout joins a frontmatter entry with its body section.
type Workout struct {
	Meta    WorkoutMeta
	Prose   string // markdown text between the heading and the fence (trimmed)
	DSL     string // contents of the ```fit-workout fence (trimmed)
	Heading string // verbatim heading line, e.g. "## Z2 Endurance"
}

// ReadDay parses a single planned-workouts day file from disk.
func ReadDay(path string) (*Day, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read planned-workout: %w", err)
	}
	d, err := ParseDay(string(buf))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	d.Path = path
	return d, nil
}

// ParseDay parses raw markdown source.
func ParseDay(src string) (*Day, error) {
	frontStr, body, err := splitFrontmatter(src)
	if err != nil {
		return nil, err
	}
	var fm Frontmatter
	if frontStr != "" {
		if err := yaml.Unmarshal([]byte(frontStr), &fm); err != nil {
			return nil, fmt.Errorf("parse frontmatter: %w", err)
		}
	}
	if fm.FitAgent.Kind != "" && fm.FitAgent.Kind != "planned-workout-day" {
		return nil, fmt.Errorf("unsupported fit-agent.kind %q (want planned-workout-day)", fm.FitAgent.Kind)
	}
	sections := parseSections(body)
	day := &Day{
		Date:  fm.FitAgent.Date,
		Front: fm,
		Raw:   src,
	}
	for _, meta := range fm.Workouts {
		sec, ok := sections[meta.Name]
		if !ok {
			return nil, fmt.Errorf("workout %q has no matching '## %s' section", meta.Name, meta.Name)
		}
		day.Workouts = append(day.Workouts, Workout{
			Meta:    meta,
			Prose:   sec.prose,
			DSL:     sec.dsl,
			Heading: sec.heading,
		})
	}
	return day, nil
}

// splitFrontmatter returns the YAML block (without "---" delimiters)
// and the remaining markdown body. If the file has no frontmatter,
// returns ("", src, nil).
func splitFrontmatter(src string) (string, string, error) {
	src = strings.TrimLeft(src, "\ufeff")
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return "", src, nil
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(src, "---\r\n"), "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", fmt.Errorf("frontmatter: missing closing '---'")
	}
	front := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(strings.TrimPrefix(body, "\r\n"), "\n")
	return front, body, nil
}

type section struct {
	heading string
	prose   string
	dsl     string
}

// parseSections walks the markdown body and groups content by "## name"
// headings. Inside each section it extracts the first ```fit-workout
// fenced block as DSL and treats prose before the fence as the prose
// field. Prose after the fence is currently ignored.
func parseSections(body string) map[string]section {
	out := map[string]section{}
	var (
		curName string
		cur     section
		lines   []string
	)
	flush := func() {
		if curName == "" {
			return
		}
		var prose, dsl strings.Builder
		fenceMode := 0 // 0=before, 1=inside fit-workout, 2=after
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			switch {
			case fenceMode == 0 && (t == "```fit-workout" || strings.HasPrefix(t, "```fit-workout ")):
				fenceMode = 1
			case fenceMode == 1 && t == "```":
				fenceMode = 2
			case fenceMode == 1:
				dsl.WriteString(ln)
				dsl.WriteString("\n")
			case fenceMode == 0:
				prose.WriteString(ln)
				prose.WriteString("\n")
			}
		}
		cur.prose = strings.TrimSpace(prose.String())
		cur.dsl = strings.TrimRight(dsl.String(), "\n")
		out[curName] = cur
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inFence := false
	for scanner.Scan() {
		ln := scanner.Text()
		t := strings.TrimSpace(ln)
		// Track any triple-backtick fence so a "##" inside code isn't
		// mistaken for a heading.
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			if curName != "" {
				lines = append(lines, ln)
			}
			continue
		}
		if !inFence && strings.HasPrefix(ln, "## ") {
			flush()
			curName = strings.TrimSpace(strings.TrimPrefix(ln, "## "))
			cur = section{heading: ln}
			lines = nil
			continue
		}
		if curName != "" {
			lines = append(lines, ln)
		}
	}
	flush()
	return out
}

// StampEventID rewrites src so that the workout matching name has
// "icu_event_id: <id>" set in its frontmatter entry. It preserves
// surrounding formatting; if the line is currently "icu_event_id: null"
// it replaces just the value, otherwise it inserts the line after the
// matching name: line. The returned string is the new file contents.
//
// This is intentionally a textual edit (not a YAML round-trip) so that
// human-authored comments and quoting style are preserved.
func StampEventID(src, name string, id int64) (string, error) {
	front, body, err := splitFrontmatter(src)
	if err != nil {
		return "", err
	}
	if front == "" {
		return "", fmt.Errorf("no frontmatter to stamp into")
	}
	newFront, err := stampInFront(front, name, id)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	out.WriteString(newFront)
	if !strings.HasSuffix(newFront, "\n") {
		out.WriteString("\n")
	}
	out.WriteString("---\n")
	out.WriteString(body)
	return out.String(), nil
}

// stampInFront finds the entry under workouts: with the given name and
// sets/replaces icu_event_id on it.
func stampInFront(front, name string, id int64) (string, error) {
	lines := strings.Split(front, "\n")
	// Locate the workout entry: a line "- name:" matching the target.
	target := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "- name:") && !strings.HasPrefix(t, "-name:") {
			continue
		}
		// Extract the value, stripping quotes.
		val := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, "- name:"), "-name:"))
		val = strings.Trim(val, `"' `)
		if val == name {
			target = i
			break
		}
	}
	if target < 0 {
		return "", fmt.Errorf("workout %q not found in frontmatter", name)
	}
	// Determine the indent of subsequent fields. Mapping items continue at
	// itemIndent + len("- ") = itemIndent + 2.
	itemIndent := leadingSpaces(lines[target])
	fieldIndent := strings.Repeat(" ", len(itemIndent)+2)
	// Look for an existing icu_event_id within this list item (until the
	// next "- " at the same indent or end).
	idLine := fmt.Sprintf("%sicu_event_id: %d", fieldIndent, id)
	for j := target + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if strings.HasPrefix(t, "- ") && leadingSpaces(lines[j]) == itemIndent {
			break
		}
		if strings.HasPrefix(t, "icu_event_id:") {
			lines[j] = idLine
			return strings.Join(lines, "\n"), nil
		}
	}
	// No existing field: insert after target line.
	out := append([]string{}, lines[:target+1]...)
	out = append(out, idLine)
	out = append(out, lines[target+1:]...)
	return strings.Join(out, "\n"), nil
}

func leadingSpaces(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			return s[:i]
		}
	}
	return s
}
