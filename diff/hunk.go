package diff

import "fmt"

// noNewlineMarker is the line diff -u inserts after a line that is
// the source file's last line and lacks a trailing newline.
const noNewlineMarker = `\ No newline at end of file`

// hunkRange is one hunk's [start, end) index range into the full ops
// slice.
type hunkRange struct {
	start, end int
}

// groupHunks finds each maximal contiguous run of non-equal ops,
// expands it by context lines of equal ops on each side, and merges
// any two expanded ranges that touch or overlap. Two change runs
// merge when they land within 2*context lines of each other,
// matching GNU diff's default grouping.
func groupHunks(ops []op, context int) []hunkRange {
	n := len(ops)
	var runs []hunkRange
	i := 0
	for i < n {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := i
		for i < n && ops[i].kind != ' ' {
			i++
		}
		runs = append(runs, hunkRange{start: start, end: i})
	}
	if len(runs) == 0 {
		return nil
	}

	var hunks []hunkRange
	cur := hunkRange{start: max(0, runs[0].start-context), end: min(n, runs[0].end+context)}
	for k := 1; k < len(runs); k++ {
		next := hunkRange{start: max(0, runs[k].start-context), end: min(n, runs[k].end+context)}
		if next.start <= cur.end {
			cur.end = max(cur.end, next.end)
			continue
		}
		hunks = append(hunks, cur)
		cur = next
	}
	hunks = append(hunks, cur)
	return hunks
}

// renderUnified formats hunks as unified-diff text lines, including
// the "---"/"+++" file headers and each hunk's "@@" header.
func renderUnified(name string, ops []op, hunks []hunkRange, aLen, bLen int, aTrailing, bTrailing bool) []string {
	lines := []string{"--- " + name, "+++ " + name}
	for _, h := range hunks {
		hunkOps := ops[h.start:h.end]
		aStart, aCount, bStart, bCount := hunkHeader(ops, h)
		lines = append(lines, fmt.Sprintf("@@ -%s +%s @@", rangeText(aStart, aCount), rangeText(bStart, bCount)))
		for _, o := range hunkOps {
			lines = append(lines, string(o.kind)+o.text)
			if noNewlineAfter(o, aLen, bLen, aTrailing, bTrailing) {
				lines = append(lines, noNewlineMarker)
			}
		}
	}
	return lines
}

// hunkHeader computes the "@@ -aStart,aCount +bStart,bCount @@"
// values for one hunk, looking back through ops before the hunk when
// the hunk holds no line from a or from b (a pure insert or pure
// delete at the start of the file).
func hunkHeader(ops []op, h hunkRange) (aStart, aCount, bStart, bCount int) {
	for _, o := range ops[h.start:h.end] {
		if o.aIdx != -1 {
			aCount++
		}
		if o.bIdx != -1 {
			bCount++
		}
	}
	aStart = firstIndex(ops[h.start:h.end], true)
	bStart = firstIndex(ops[h.start:h.end], false)
	if aStart == -1 {
		aStart = lastIndexBefore(ops, h.start, true) + 1
	} else {
		aStart++
	}
	if bStart == -1 {
		bStart = lastIndexBefore(ops, h.start, false) + 1
	} else {
		bStart++
	}
	return aStart, aCount, bStart, bCount
}

// firstIndex returns the first aIdx (isA true) or bIdx (isA false)
// present in hunkOps, or -1 when none is present.
func firstIndex(hunkOps []op, isA bool) int {
	for _, o := range hunkOps {
		idx := o.bIdx
		if isA {
			idx = o.aIdx
		}
		if idx != -1 {
			return idx
		}
	}
	return -1
}

// lastIndexBefore returns the aIdx (isA true) or bIdx (isA false) of
// the last op before position start that carries one, or -1 when none
// exists.
func lastIndexBefore(ops []op, start int, isA bool) int {
	for i := start - 1; i >= 0; i-- {
		idx := ops[i].bIdx
		if isA {
			idx = ops[i].aIdx
		}
		if idx != -1 {
			return idx
		}
	}
	return -1
}

// rangeText formats one side of a hunk header: a bare line number
// when count is 1, otherwise "start,count".
func rangeText(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

// noNewlineAfter reports whether op is the last line of a or b and
// that side lacks a trailing newline, so the rendered output needs
// diff -u's "no newline" marker right after it.
func noNewlineAfter(o op, aLen, bLen int, aTrailing, bTrailing bool) bool {
	switch o.kind {
	case '-':
		return o.aIdx == aLen-1 && !aTrailing
	case '+':
		return o.bIdx == bLen-1 && !bTrailing
	default:
		return o.aIdx == aLen-1 && o.bIdx == bLen-1 && !aTrailing && !bTrailing
	}
}
