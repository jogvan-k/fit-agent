package render

import (
	"fmt"
	"strings"
)

// formatFloat formats f with prec decimal places, trimming trailing
// zeros while keeping at least one digit after the decimal point. This
// keeps integers as "9800.0" rather than "9800" so the unit suffix
// (e.g. "_m") still reads as a float in the file.
func formatFloat(f float64, prec int) string {
	s := fmt.Sprintf("%.*f", prec, f)
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "0"
	}
	return s
}

// yamlString returns a safe YAML scalar for s.
//
// We always quote with double quotes when the string contains characters
// YAML 1.2 might interpret specially: leading/trailing whitespace,
// control characters, leading indicators (@, `, !, &, *, ?, |, >, %,
// #), or the unquoted reserved words (true/false/null/yes/...) and
// numeric-looking forms.
func yamlString(s string) string {
	if needsQuote(s) {
		return strconvQuote(s)
	}
	return s
}

func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	switch strings.ToLower(s) {
	case "true", "false", "null", "yes", "no", "on", "off", "~":
		return true
	}
	if isNumericLooking(s) {
		return true
	}
	for i, r := range s {
		switch r {
		case ':', '#', '&', '*', '!', '|', '>', '%', '@', '`',
			'{', '}', '[', ']', ',', '"', '\'', '\\', '\n', '\t':
			return true
		case ' ':
			if i == 0 || i == len(s)-1 {
				return true
			}
		}
		if r < 0x20 {
			return true
		}
	}
	if s[0] == '-' || s[0] == '?' {
		return true
	}
	return false
}

func isNumericLooking(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' && r != 'e' && r != 'E' {
			return false
		}
	}
	return true
}

// strconvQuote double-quotes s with minimal escaping: backslash, double
// quote, and the common control sequences.
func strconvQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// yamlBlockScalar emits s as a literal block scalar (`|`) suitable for
// multi-line text like athlete notes. keyCol is the column at which
// the *key* sits (e.g. 0 for a top-level key, 4 for a key two levels
// deep); the helper indents value lines two columns further, which is
// the YAML default.
//
// Trailing newlines on s are stripped; the block-scalar form already
// implies a single newline at the end after re-parse.
func yamlBlockScalar(s string, keyCol int) string {
	pad := strings.Repeat(" ", keyCol+2)
	var b strings.Builder
	b.WriteString("|\n")
	for i, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(pad)
		b.WriteString(line)
	}
	return b.String()
}
