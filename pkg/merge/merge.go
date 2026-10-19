package merge

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/tree"
)

// Conflict describes a merge conflict on a specific file
type Conflict struct {
	Path        string `json:"path"`
	Reason      string `json:"reason"`
	BaseHash    string `json:"base_hash"`
	OursHash    string `json:"ours_hash"`
	TheirsHash  string `json:"theirs_hash"`
}

// Result holds the outcome of a 3-way merge
type Result struct {
	Clean      bool       `json:"clean"`
	MergedTree *tree.Tree `json:"merged_tree"`
	Conflicts  []Conflict `json:"conflicts,omitempty"`
}

// ThreeWayMerge merges 'theirs' onto 'ours' with respect to common ancestor 'base'
func ThreeWayMerge(base, ours, theirs *tree.Tree, store *cas.Store) (*Result, error) {
	merged := tree.NewTree()
	var conflicts []Conflict

	baseEntries := make(map[string]tree.Entry)
	if base != nil {
		baseEntries = base.Entries
	}

	oursEntries := make(map[string]tree.Entry)
	if ours != nil {
		oursEntries = ours.Entries
	}

	theirsEntries := make(map[string]tree.Entry)
	if theirs != nil {
		theirsEntries = theirs.Entries
	}

	// Union of all paths across base, ours, theirs
	allPaths := make(map[string]bool)
	for p := range baseEntries {
		allPaths[p] = true
	}
	for p := range oursEntries {
		allPaths[p] = true
	}
	for p := range theirsEntries {
		allPaths[p] = true
	}

	for p := range allPaths {
		baseEntry, inBase := baseEntries[p]
		oursEntry, inOurs := oursEntries[p]
		theirsEntry, inTheirs := theirsEntries[p]

		// Case 1: Unchanged in theirs -> Keep ours
		if inBase && inTheirs && baseEntry.Hash == theirsEntry.Hash {
			if inOurs {
				merged.Entries[p] = oursEntry
			}
			continue
		}

		// Case 2: Unchanged in ours -> Take theirs
		if inBase && inOurs && baseEntry.Hash == oursEntry.Hash {
			if inTheirs {
				merged.Entries[p] = theirsEntry
			}
			continue
		}

		// Case 3: Added in theirs, not in base or ours -> Accept theirs
		if !inBase && !inOurs && inTheirs {
			merged.Entries[p] = theirsEntry
			continue
		}

		// Case 4: Added in ours, not in base or theirs -> Keep ours
		if !inBase && inOurs && !inTheirs {
			merged.Entries[p] = oursEntry
			continue
		}

		// Case 5: Deleted in theirs, unchanged in ours -> Delete
		if inBase && !inTheirs && inOurs && baseEntry.Hash == oursEntry.Hash {
			// omit from merged
			continue
		}

		// Case 6: Deleted in ours, unchanged in theirs -> Delete
		if inBase && !inOurs && inTheirs && baseEntry.Hash == theirsEntry.Hash {
			// omit from merged
			continue
		}

		// Case 7: Both modified to identical content -> Take either
		if inOurs && inTheirs && oursEntry.Hash == theirsEntry.Hash {
			merged.Entries[p] = oursEntry
			continue
		}

		// Case 8: Both modified to DIFFERENT content -> Attempt line-level 3-way merge
		if inOurs && inTheirs && oursEntry.Hash != theirsEntry.Hash {
			mergedEntry, clean, err := mergeFileBlobs(p, baseEntry, oursEntry, theirsEntry, store)
			if err != nil {
				return nil, fmt.Errorf("failed to merge file %s: %w", p, err)
			}

			if clean {
				merged.Entries[p] = *mergedEntry
			} else {
				conflicts = append(conflicts, Conflict{
					Path:       p,
					Reason:     "Overlapping line modifications in concurrent edits",
					BaseHash:   baseEntry.Hash,
					OursHash:   oursEntry.Hash,
					TheirsHash: theirsEntry.Hash,
				})
				// Still put ours in tree as fallback
				merged.Entries[p] = oursEntry
			}
			continue
		}

		// Case 9: Delete/Modify conflict
		conflicts = append(conflicts, Conflict{
			Path:       p,
			Reason:     "Delete/Modify conflict between branches",
			BaseHash:   baseEntry.Hash,
			OursHash:   oursEntry.Hash,
			TheirsHash: theirsEntry.Hash,
		})
		if inOurs {
			merged.Entries[p] = oursEntry
		}
	}

	merged.ComputeHash()

	return &Result{
		Clean:      len(conflicts) == 0,
		MergedTree: merged,
		Conflicts:  conflicts,
	}, nil
}

// mergeFileBlobs attempts to read blobs and perform line-based 3-way merge
func mergeFileBlobs(
	path string,
	baseEntry, oursEntry, theirsEntry tree.Entry,
	store *cas.Store,
) (*tree.Entry, bool, error) {
	baseData, _ := store.GetBlobByHash(baseEntry.Hash)
	oursData, err := store.GetBlobByHash(oursEntry.Hash)
	if err != nil {
		return nil, false, err
	}
	theirsData, err := store.GetBlobByHash(theirsEntry.Hash)
	if err != nil {
		return nil, false, err
	}

	mergedContent, clean := mergeLines(baseData, oursData, theirsData)
	if !clean {
		return nil, false, nil
	}

	manifest, err := store.PutBlob(mergedContent)
	if err != nil {
		return nil, false, err
	}

	entry := &tree.Entry{
		Path: path,
		Type: tree.TypeBlob,
		Hash: manifest.BlobHash,
		Size: manifest.TotalSize,
		Mode: oursEntry.Mode,
	}
	return entry, true, nil
}

// mergeLines performs a simple, effective 3-way line merge
func mergeLines(base, ours, theirs []byte) ([]byte, bool) {
	// If ours or theirs is identical to base
	if bytes.Equal(ours, base) {
		return theirs, true
	}
	if bytes.Equal(theirs, base) {
		return ours, true
	}
	if bytes.Equal(ours, theirs) {
		return ours, true
	}

	baseLines := splitLines(string(base))
	oursLines := splitLines(string(ours))
	theirsLines := splitLines(string(theirs))

	// Simple fast path: Append-only checks or prefix/suffix checks
	// If base was empty (new file on both sides)
	if len(baseLines) == 0 {
		return nil, false
	}

	// Line-by-line reconciliation
	var out []string
	clean := true

	maxLen := len(baseLines)
	if len(oursLines) > maxLen {
		maxLen = len(oursLines)
	}
	if len(theirsLines) > maxLen {
		maxLen = len(theirsLines)
	}

	// Check if changes are non-overlapping chunks
	// If line counts match exactly:
	if len(baseLines) == len(oursLines) && len(baseLines) == len(theirsLines) {
		for i := 0; i < len(baseLines); i++ {
			b := baseLines[i]
			o := oursLines[i]
			t := theirsLines[i]

			if o == b && t == b {
				out = append(out, b)
			} else if o != b && t == b {
				out = append(out, o)
			} else if t != b && o == b {
				out = append(out, t)
			} else if o == t {
				out = append(out, o)
			} else {
				// Overlap conflict
				clean = false
				out = append(out, "<<<<<<< OURS (MAIN)")
				out = append(out, o)
				out = append(out, "=======")
				out = append(out, t)
				out = append(out, ">>>>>>> THEIRS (SUBMITTED)")
			}
		}
		var buf bytes.Buffer
		for i, l := range out {
			buf.WriteString(l)
			if i < len(out)-1 || strings.HasSuffix(string(ours), "\n") {
				buf.WriteString("\n")
			}
		}
		return buf.Bytes(), clean
	}

	// For general case with different line lengths:
	// If one modified the header and the other modified the footer:
	// Find common prefix and common suffix with base
	prefixOurs := commonPrefix(baseLines, oursLines)
	prefixTheirs := commonPrefix(baseLines, theirsLines)
	suffixOurs := commonSuffix(baseLines, oursLines)
	suffixTheirs := commonSuffix(baseLines, theirsLines)

	// If ours modified tail and theirs modified head:
	if prefixOurs == len(baseLines) && suffixTheirs == len(baseLines) {
		res := append([]string{}, theirsLines[:len(theirsLines)-suffixTheirs]...)
		res = append(res, baseLines...)
		res = append(res, oursLines[prefixOurs:]...)
		return []byte(strings.Join(res, "\n") + "\n"), true
	}

	// If theirs modified tail and ours modified head:
	if prefixTheirs == len(baseLines) && suffixOurs == len(baseLines) {
		res := append([]string{}, oursLines[:len(oursLines)-suffixOurs]...)
		res = append(res, baseLines...)
		res = append(res, theirsLines[prefixTheirs:]...)
		return []byte(strings.Join(res, "\n") + "\n"), true
	}

	// Conflict fallback
	return nil, false
}

func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func commonPrefix(a, b []string) int {
	min := len(a)
	if len(b) < min {
		min = len(b)
	}
	for i := 0; i < min; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min
}

func commonSuffix(a, b []string) int {
	min := len(a)
	if len(b) < min {
		min = len(b)
	}
	for i := 0; i < min; i++ {
		if a[len(a)-1-i] != b[len(b)-1-i] {
			return i
		}
	}
	return min
}
