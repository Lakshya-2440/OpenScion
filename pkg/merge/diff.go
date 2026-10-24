package merge

import (
	"bytes"
	"fmt"
	"strings"
)

// EditType represents the type of diff operation in Myers diff
type EditType int

const (
	EditEqual EditType = iota
	EditInsert
	EditDelete
)

// Edit represents an individual line operation
type Edit struct {
	Type EditType
	Line string
}

// DiffHunk represents a contiguous modified region in Base replaced by Lines
type DiffHunk struct {
	BaseStart int      // 0-indexed start line in Base
	BaseEnd   int      // 0-indexed end line in Base (exclusive)
	Lines     []string // Replacement lines from the other version
}

// isBinary checks if a byte slice contains NUL bytes in its first 8KB
func isBinary(data []byte) bool {
	limit := len(data)
	if limit > 8000 {
		limit = 8000
	}
	return bytes.IndexByte(data[:limit], 0) != -1
}

// MyersDiff computes the shortest edit script (SES) from slice a to slice b
func MyersDiff(a, b []string) []Edit {
	n := len(a)
	m := len(b)

	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		edits := make([]Edit, m)
		for i, l := range b {
			edits[i] = Edit{Type: EditInsert, Line: l}
		}
		return edits
	}
	if m == 0 {
		edits := make([]Edit, n)
		for i, l := range a {
			edits[i] = Edit{Type: EditDelete, Line: l}
		}
		return edits
	}

	maxD := n + m
	offset := maxD
	v := make([]int, 2*maxD+1)
	history := make([][]int, 0, maxD+1)

	// D = 0: match common prefix
	x := 0
	y := 0
	for x < n && y < m && a[x] == b[y] {
		x++
		y++
	}
	v[offset] = x
	vCopy := make([]int, len(v))
	copy(vCopy, v)
	history = append(history, vCopy)

	if x >= n && y >= m {
		// All lines match
		edits := make([]Edit, n)
		for i, l := range a {
			edits[i] = Edit{Type: EditEqual, Line: l}
		}
		return edits
	}

	done := false
	for d := 1; d <= maxD; d++ {
		for k := -d; k <= d; k += 2 {
			var prevX int
			if k == -d || (k != d && v[k-1+offset] < v[k+1+offset]) {
				prevX = v[k+1+offset] // move down (insert)
			} else {
				prevX = v[k-1+offset] + 1 // move right (delete)
			}
			currX := prevX
			currY := currX - k
			for currX < n && currY < m && a[currX] == b[currY] {
				currX++
				currY++
			}
			v[k+offset] = currX
			if currX >= n && currY >= m {
				vSave := make([]int, len(v))
				copy(vSave, v)
				history = append(history, vSave)
				done = true
				break
			}
		}
		if done {
			break
		}
		vSave := make([]int, len(v))
		copy(vSave, v)
		history = append(history, vSave)
	}

	return backtrackMyers(history, a, b, offset)
}

func backtrackMyers(history [][]int, a, b []string, offset int) []Edit {
	n := len(a)
	m := len(b)
	d := len(history) - 1

	x := n
	y := m
	var edits []Edit

	for d > 0 {
		k := x - y
		prevV := history[d-1]

		var prevK int
		if k == -d || (k != d && prevV[k-1+offset] < prevV[k+1+offset]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}

		prevX := prevV[prevK+offset]
		prevY := prevX - prevK

		// Common snake (equal lines)
		for x > prevX && y > prevY && x > 0 && y > 0 && a[x-1] == b[y-1] {
			edits = append(edits, Edit{Type: EditEqual, Line: a[x-1]})
			x--
			y--
		}

		if d > 0 {
			if x == prevX {
				// Insert from b
				if y > 0 {
					edits = append(edits, Edit{Type: EditInsert, Line: b[y-1]})
					y--
				}
			} else {
				// Delete from a
				if x > 0 {
					edits = append(edits, Edit{Type: EditDelete, Line: a[x-1]})
					x--
				}
			}
		}
		d--
	}

	// Remaining common prefix from d = 0
	for x > 0 && y > 0 && a[x-1] == b[y-1] {
		edits = append(edits, Edit{Type: EditEqual, Line: a[x-1]})
		x--
		y--
	}

	// Reverse edits since we backtracked
	for i, j := 0, len(edits)-1; i < j; i, j = i+1, j-1 {
		edits[i], edits[j] = edits[j], edits[i]
	}

	return edits
}

// editsToHunks converts Myers diff edits from Base into contiguous change hunks
func editsToHunks(edits []Edit) []DiffHunk {
	var hunks []DiffHunk
	baseIdx := 0

	i := 0
	for i < len(edits) {
		if edits[i].Type == EditEqual {
			baseIdx++
			i++
			continue
		}

		hunkStart := baseIdx
		var lines []string

		for i < len(edits) && edits[i].Type != EditEqual {
			if edits[i].Type == EditDelete {
				baseIdx++
			} else if edits[i].Type == EditInsert {
				lines = append(lines, edits[i].Line)
			}
			i++
		}

		hunks = append(hunks, DiffHunk{
			BaseStart: hunkStart,
			BaseEnd:   baseIdx,
			Lines:     lines,
		})
	}

	return hunks
}

// Diff3Merge reconciles Ours and Theirs changes against Base line-by-line
func Diff3Merge(baseLines, oursLines, theirsLines []string) ([]string, bool) {
	// Fast path: identical lines
	if slicesEqual(oursLines, theirsLines) {
		return oursLines, true
	}
	if slicesEqual(oursLines, baseLines) {
		return theirsLines, true
	}
	if slicesEqual(theirsLines, baseLines) {
		return oursLines, true
	}

	// Handle empty base
	if len(baseLines) == 0 {
		if slicesEqual(oursLines, theirsLines) {
			return oursLines, true
		}
		// Both added different content to empty file -> conflict
		var out []string
		out = append(out, "<<<<<<< OURS (MAIN)")
		out = append(out, oursLines...)
		out = append(out, "=======")
		out = append(out, theirsLines...)
		out = append(out, ">>>>>>> THEIRS (SUBMITTED)")
		return out, false
	}

	oursHunks := editsToHunks(MyersDiff(baseLines, oursLines))
	theirsHunks := editsToHunks(MyersDiff(baseLines, theirsLines))

	var result []string
	clean := true
	cursor := 0
	oi, ti := 0, 0

	for oi < len(oursHunks) || ti < len(theirsHunks) {
		// Only ours left
		if ti >= len(theirsHunks) {
			h := oursHunks[oi]
			if h.BaseStart > cursor {
				result = append(result, baseLines[cursor:h.BaseStart]...)
			}
			result = append(result, h.Lines...)
			cursor = h.BaseEnd
			oi++
			continue
		}

		// Only theirs left
		if oi >= len(oursHunks) {
			h := theirsHunks[ti]
			if h.BaseStart > cursor {
				result = append(result, baseLines[cursor:h.BaseStart]...)
			}
			result = append(result, h.Lines...)
			cursor = h.BaseEnd
			ti++
			continue
		}

		hO := oursHunks[oi]
		hT := theirsHunks[ti]

		// Check overlap between hO and hT
		overlaps := checkHunkOverlap(hO, hT)

		if !overlaps {
			// Advance whichever hunk comes earlier in Base
			if isHunkEarlier(hO, hT) {
				if hO.BaseStart > cursor {
					result = append(result, baseLines[cursor:hO.BaseStart]...)
				}
				result = append(result, hO.Lines...)
				cursor = hO.BaseEnd
				oi++
			} else {
				if hT.BaseStart > cursor {
					result = append(result, baseLines[cursor:hT.BaseStart]...)
				}
				result = append(result, hT.Lines...)
				cursor = hT.BaseEnd
				ti++
			}
			continue
		}

		// Overlapping cluster: gather all connected hunks from both sides
		clusterStart := minInt(hO.BaseStart, hT.BaseStart)
		clusterEnd := maxInt(hO.BaseEnd, hT.BaseEnd)

		clusterOursStartIdx := oi
		clusterTheirsStartIdx := ti

		oi++
		ti++

		// Expand cluster if subsequent hunks also overlap
		for {
			expanded := false
			if oi < len(oursHunks) && checkRangeOverlap(clusterStart, clusterEnd, oursHunks[oi].BaseStart, oursHunks[oi].BaseEnd) {
				clusterStart = minInt(clusterStart, oursHunks[oi].BaseStart)
				clusterEnd = maxInt(clusterEnd, oursHunks[oi].BaseEnd)
				oi++
				expanded = true
			}
			if ti < len(theirsHunks) && checkRangeOverlap(clusterStart, clusterEnd, theirsHunks[ti].BaseStart, theirsHunks[ti].BaseEnd) {
				clusterStart = minInt(clusterStart, theirsHunks[ti].BaseStart)
				clusterEnd = maxInt(clusterEnd, theirsHunks[ti].BaseEnd)
				ti++
				expanded = true
			}
			if !expanded {
				break
			}
		}

		// Append base lines before this cluster
		if clusterStart > cursor {
			result = append(result, baseLines[cursor:clusterStart]...)
		}

		// Reconstruct what Ours produced in [clusterStart, clusterEnd]
		oursSub := applyHunksToRange(baseLines, oursHunks[clusterOursStartIdx:oi], clusterStart, clusterEnd)
		// Reconstruct what Theirs produced in [clusterStart, clusterEnd]
		theirsSub := applyHunksToRange(baseLines, theirsHunks[clusterTheirsStartIdx:ti], clusterStart, clusterEnd)

		if slicesEqual(oursSub, theirsSub) {
			// Identical concurrent edits -> clean merge!
			result = append(result, oursSub...)
		} else {
			// Conflict!
			clean = false
			result = append(result, "<<<<<<< OURS (MAIN)")
			result = append(result, oursSub...)
			result = append(result, "=======")
			result = append(result, theirsSub...)
			result = append(result, ">>>>>>> THEIRS (SUBMITTED)")
		}

		cursor = clusterEnd
	}

	// Append remaining base lines
	if cursor < len(baseLines) {
		result = append(result, baseLines[cursor:]...)
	}

	return result, clean
}

func checkHunkOverlap(a, b DiffHunk) bool {
	return checkRangeOverlap(a.BaseStart, a.BaseEnd, b.BaseStart, b.BaseEnd)
}

func checkRangeOverlap(s1, e1, s2, e2 int) bool {
	// Both are pure insertions
	if s1 == e1 && s2 == e2 {
		return s1 == s2
	}
	// One is insertion
	if s1 == e1 {
		return s2 <= s1 && s1 < e2
	}
	if s2 == e2 {
		return s1 <= s2 && s2 < e1
	}
	// General interval intersection
	return maxInt(s1, s2) < minInt(e1, e2)
}

func isHunkEarlier(a, b DiffHunk) bool {
	if a.BaseStart != b.BaseStart {
		return a.BaseStart < b.BaseStart
	}
	return a.BaseEnd < b.BaseEnd
}

func applyHunksToRange(base []string, hunks []DiffHunk, start, end int) []string {
	var out []string
	pos := start
	for _, h := range hunks {
		if h.BaseStart > pos {
			out = append(out, base[pos:h.BaseStart]...)
		}
		out = append(out, h.Lines...)
		pos = h.BaseEnd
	}
	if pos < end {
		out = append(out, base[pos:end]...)
	}
	return out
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// UnifiedHunk represents a unified diff hunk for display
type UnifiedHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string // With prefix ' ', '+', or '-'
}

// GenerateUnifiedHunks produces hunks with given context lines from edits
func GenerateUnifiedHunks(edits []Edit, contextLines int) []UnifiedHunk {
	if len(edits) == 0 {
		return nil
	}
	if contextLines < 0 {
		contextLines = 3
	}

	// Filter whether there are any actual changes
	hasChanges := false
	for _, e := range edits {
		if e.Type != EditEqual {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return nil
	}

	// Track old and new line numbers for each edit
	type editPos struct {
		edit    Edit
		oldLine int
		newLine int
	}

	positions := make([]editPos, len(edits))
	oldLine := 1
	newLine := 1
	for i, e := range edits {
		positions[i] = editPos{edit: e, oldLine: oldLine, newLine: newLine}
		switch e.Type {
		case EditEqual:
			oldLine++
			newLine++
		case EditDelete:
			oldLine++
		case EditInsert:
			newLine++
		}
	}

	var hunks []UnifiedHunk
	n := len(positions)
	i := 0
	for i < n {
		// Advance to next change
		for i < n && positions[i].edit.Type == EditEqual {
			i++
		}
		if i >= n {
			break
		}

		// Change starts at or before i, expand backwards by contextLines
		startIdx := i - contextLines
		if startIdx < 0 {
			startIdx = 0
		}

		// Find end of this hunk group (merge changes that are <= 2*contextLines apart)
		endIdx := i
		for endIdx < n {
			if positions[endIdx].edit.Type != EditEqual {
				i = endIdx
			} else if endIdx-i > 2*contextLines {
				break
			}
			endIdx++
		}

		// Expand forwards by contextLines
		lastChangeIdx := i
		hunkEnd := lastChangeIdx + contextLines + 1
		if hunkEnd > n {
			hunkEnd = n
		}

		// Build hunk
		var hunkLines []string
		oldCount := 0
		newCount := 0
		oldStart := 0
		newStart := 0

		for idx := startIdx; idx < hunkEnd; idx++ {
			p := positions[idx]
			if oldStart == 0 && (p.edit.Type == EditEqual || p.edit.Type == EditDelete) {
				oldStart = p.oldLine
			}
			if newStart == 0 && (p.edit.Type == EditEqual || p.edit.Type == EditInsert) {
				newStart = p.newLine
			}

			switch p.edit.Type {
			case EditEqual:
				hunkLines = append(hunkLines, " "+p.edit.Line)
				oldCount++
				newCount++
			case EditDelete:
				hunkLines = append(hunkLines, "-"+p.edit.Line)
				oldCount++
			case EditInsert:
				hunkLines = append(hunkLines, "+"+p.edit.Line)
				newCount++
			}
		}

		if oldStart == 0 {
			oldStart = 1
		}
		if newStart == 0 {
			newStart = 1
		}

		hunks = append(hunks, UnifiedHunk{
			OldStart: oldStart,
			OldCount: oldCount,
			NewStart: newStart,
			NewCount: newCount,
			Lines:    hunkLines,
		})

		i = hunkEnd
	}

	return hunks
}

// FormatUnifiedDiff formats Myers diff edits into unified diff format
func FormatUnifiedDiff(path string, a, b []string, colorize bool) string {
	edits := MyersDiff(a, b)
	hunks := GenerateUnifiedHunks(edits, 3)
	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	red := "\033[31m"
	green := "\033[32m"
	cyan := "\033[36m"
	reset := "\033[0m"

	if !colorize {
		red, green, cyan, reset = "", "", "", ""
	}

	sb.WriteString(fmt.Sprintf("%s--- a/%s%s\n", red, path, reset))
	sb.WriteString(fmt.Sprintf("%s+++ b/%s%s\n", green, path, reset))

	for _, h := range hunks {
		sb.WriteString(fmt.Sprintf("  %s@@ -%d,%d +%d,%d @@%s\n",
			cyan, h.OldStart, h.OldCount, h.NewStart, h.NewCount, reset))
		for _, l := range h.Lines {
			if len(l) == 0 {
				continue
			}
			prefix := l[0]
			content := l[1:]
			switch prefix {
			case '-':
				sb.WriteString(fmt.Sprintf("%s-%s%s\n", red, reset, content))
			case '+':
				sb.WriteString(fmt.Sprintf("%s+%s%s\n", green, reset, content))
			default:
				sb.WriteString(fmt.Sprintf(" %s%s\n", reset, content))
			}
		}
	}
	return sb.String()
}
