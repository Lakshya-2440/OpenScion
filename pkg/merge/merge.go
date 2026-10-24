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

	// Detect structural file vs directory collisions
	for p := range merged.Entries {
		prefix := p + "/"
		for other := range merged.Entries {
			if strings.HasPrefix(other, prefix) {
				conflicts = append(conflicts, Conflict{
					Path:   p,
					Reason: fmt.Sprintf("Structural conflict: %q exists as both a file and directory (%s)", p, other),
				})
			}
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
		if len(mergedContent) > 0 {
			manifest, err := store.PutBlob(mergedContent)
			if err == nil {
				return &tree.Entry{
					Path: path,
					Type: tree.TypeBlob,
					Hash: manifest.BlobHash,
					Size: manifest.TotalSize,
					Mode: oursEntry.Mode,
				}, false, nil
			}
		}
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

// mergeLines performs a comprehensive diff-based 3-way line merge
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

	// Check if any file is binary
	if isBinary(base) || isBinary(ours) || isBinary(theirs) {
		return nil, false
	}

	baseLines := splitLines(string(base))
	oursLines := splitLines(string(ours))
	theirsLines := splitLines(string(theirs))

	mergedLines, clean := Diff3Merge(baseLines, oursLines, theirsLines)

	eol := "\n"
	if bytes.Contains(ours, []byte("\r\n")) || bytes.Contains(theirs, []byte("\r\n")) {
		eol = "\r\n"
	}

	var buf bytes.Buffer
	for i, l := range mergedLines {
		buf.WriteString(l)
		if i < len(mergedLines)-1 || strings.HasSuffix(string(ours), "\n") || strings.HasSuffix(string(theirs), "\n") {
			buf.WriteString(eol)
		}
	}
	return buf.Bytes(), clean
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
