package diff

import (
	"bytes"
	"errors"
	"strings"
)

// ErrTooLarge reports that a rendered diff would exceed maxLines.
var ErrTooLarge = errors.New("diff: exceeds max lines")

// contextLines is the number of unchanged lines kept on each side of
// a change, matching GNU diff's default (N = 3).
const contextLines = 3

// Unified computes a line-level diff between a and b and formats it
// as a standard unified diff: "--- name", "+++ name", "@@" hunk
// headers, and " "/"-"/"+" line prefixes. Identical input returns an
// empty string and a nil error. maxLines bounds the rendered output's
// line count, header lines and hunk lines together; maxLines <= 0
// means no bound. Unified returns ErrTooLarge instead of a truncated
// result when the bound is exceeded. Unified has no input-size bound;
// a caller bounds len(a) and len(b) before calling.
func Unified(name string, a, b []byte, maxLines int) (string, error) {
	if bytes.Equal(a, b) {
		return "", nil
	}

	aLines, aTrailing := splitLines(a)
	bLines, bTrailing := splitLines(b)

	ops := computeOps(aLines, bLines)
	ops = splitTrailingNewlineDiff(ops, len(aLines), len(bLines), aTrailing, bTrailing)
	hunks := groupHunks(ops, contextLines)
	if len(hunks) == 0 {
		return "", nil
	}

	lines := renderUnified(name, ops, hunks, len(aLines), len(bLines), aTrailing, bTrailing)
	if maxLines > 0 && len(lines) > maxLines {
		return "", ErrTooLarge
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// splitTrailingNewlineDiff turns a matched final equal line into a
// delete/insert pair when a and b's last lines have the same text but
// differ in trailing-newline status, matching diff -u's treatment of
// a missing final newline as a content change.
func splitTrailingNewlineDiff(ops []op, aLen, bLen int, aTrailing, bTrailing bool) []op {
	if len(ops) == 0 || aTrailing == bTrailing {
		return ops
	}
	last := ops[len(ops)-1]
	if last.kind != ' ' || last.aIdx != aLen-1 || last.bIdx != bLen-1 {
		return ops
	}
	replaced := make([]op, 0, len(ops)+1)
	replaced = append(replaced, ops[:len(ops)-1]...)
	replaced = append(replaced,
		op{kind: '-', text: last.text, aIdx: last.aIdx, bIdx: -1},
		op{kind: '+', text: last.text, aIdx: -1, bIdx: last.bIdx},
	)
	return replaced
}

// splitLines splits data on '\n' into lines and reports whether the
// last line ends with a trailing newline. Empty data yields no lines
// and a true trailing flag, since there is no last line to annotate.
func splitLines(data []byte) ([]string, bool) {
	if len(data) == 0 {
		return nil, true
	}
	parts := strings.Split(string(data), "\n")
	if parts[len(parts)-1] == "" {
		return parts[:len(parts)-1], true
	}
	return parts, false
}
