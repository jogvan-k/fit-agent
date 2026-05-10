package plannedio

import (
	"bytes"
)

// Sentinel markers — kept in sync with render.ICUBlockBegin/End. They
// are duplicated here (rather than imported) so plannedio can stay
// dependency-free from render.
const (
	icuBlockBegin = "<!-- fit-agent:icu:begin -->"
	icuBlockEnd   = "<!-- fit-agent:icu:end -->"
)

// SpliceICUBlock returns src with the machine-owned icu block replaced
// by block. If src already contains a `<!-- fit-agent:icu:begin -->`
// ... `<!-- fit-agent:icu:end -->` region, the whole region (including
// both sentinel lines) is swapped out atomically; everything outside
// the sentinels is preserved byte-for-byte.
//
// When src has no existing block the function appends block to src,
// ensuring there is exactly one blank line of separation between the
// existing content and the new block. block must already contain both
// sentinels (use [render.ICUBlock]).
//
// Both arguments may or may not end with a newline; the result always
// ends with a single newline.
//
// SpliceICUBlock does not validate the contents of block beyond
// asserting that the begin sentinel is present.
func SpliceICUBlock(src, block []byte) []byte {
	block = ensureTrailingNewline(block)

	beginIdx, endIdx, ok := findBlockBounds(src)
	if !ok {
		var out bytes.Buffer
		trimmed := bytes.TrimRight(src, "\n")
		if len(trimmed) > 0 {
			out.Write(trimmed)
			out.WriteString("\n\n")
		}
		out.Write(block)
		return out.Bytes()
	}

	var out bytes.Buffer
	out.Write(src[:beginIdx])
	out.Write(block)
	// Skip past the closing sentinel line in src, then append the
	// remainder. endIdx points at the start of the closing sentinel
	// line; advance to the end of that line.
	rest := src[endIdx:]
	if nl := bytes.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	} else {
		rest = nil
	}
	out.Write(rest)
	return ensureTrailingNewline(out.Bytes())
}

// HasICUBlock reports whether src already contains a machine-owned icu
// block.
func HasICUBlock(src []byte) bool {
	_, _, ok := findBlockBounds(src)
	return ok
}

// findBlockBounds returns the byte offsets of the start of the begin
// sentinel line and the start of the end sentinel line. ok is false
// when either sentinel is missing or they appear out of order.
func findBlockBounds(src []byte) (beginIdx, endIdx int, ok bool) {
	beginIdx = bytes.Index(src, []byte(icuBlockBegin))
	if beginIdx < 0 {
		return 0, 0, false
	}
	endIdx = bytes.Index(src[beginIdx:], []byte(icuBlockEnd))
	if endIdx < 0 {
		return 0, 0, false
	}
	endIdx += beginIdx
	// Anchor beginIdx to the start of its line so we replace the
	// entire sentinel line.
	if beginIdx > 0 {
		if nl := bytes.LastIndexByte(src[:beginIdx], '\n'); nl >= 0 {
			beginIdx = nl + 1
		} else {
			beginIdx = 0
		}
	}
	// Same for endIdx.
	if endIdx > 0 {
		if nl := bytes.LastIndexByte(src[:endIdx], '\n'); nl >= 0 {
			endIdx = nl + 1
		} else {
			endIdx = 0
		}
	}
	return beginIdx, endIdx, true
}

func ensureTrailingNewline(b []byte) []byte {
	if len(b) == 0 || b[len(b)-1] == '\n' {
		return b
	}
	out := make([]byte, len(b)+1)
	copy(out, b)
	out[len(b)] = '\n'
	return out
}
