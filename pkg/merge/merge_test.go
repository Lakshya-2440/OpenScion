package merge

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/tree"
)

func TestMyersDiff(t *testing.T) {
	a := []string{"apple", "banana", "cherry"}
	b := []string{"apple", "blueberry", "cherry", "date"}

	edits := MyersDiff(a, b)
	if len(edits) == 0 {
		t.Fatalf("expected edits, got empty")
	}

	hunks := editsToHunks(edits)
	if len(hunks) == 0 {
		t.Fatalf("expected hunks, got none")
	}
}

func TestDiff3Merge_CleanIndependentEdits(t *testing.T) {
	base := []string{
		"package main",
		"",
		"func Alpha() {",
		"    // original alpha",
		"}",
		"",
		"func Beta() {",
		"    // original beta",
		"}",
	}

	// Ours modifies Alpha
	ours := []string{
		"package main",
		"",
		"func Alpha() {",
		"    // modified alpha by ours",
		"}",
		"",
		"func Beta() {",
		"    // original beta",
		"}",
	}

	// Theirs modifies Beta and adds Gamma
	theirs := []string{
		"package main",
		"",
		"func Alpha() {",
		"    // original alpha",
		"}",
		"",
		"func Beta() {",
		"    // modified beta by theirs",
		"}",
		"",
		"func Gamma() {}",
	}

	merged, clean := Diff3Merge(base, ours, theirs)
	if !clean {
		t.Fatalf("expected clean merge, got conflicts: %v", strings.Join(merged, "\n"))
	}

	joined := strings.Join(merged, "\n")
	if !strings.Contains(joined, "modified alpha by ours") {
		t.Errorf("missing ours modification")
	}
	if !strings.Contains(joined, "modified beta by theirs") {
		t.Errorf("missing theirs modification")
	}
	if !strings.Contains(joined, "func Gamma() {}") {
		t.Errorf("missing theirs addition")
	}
}

func TestDiff3Merge_InsertionsDifferentLengths(t *testing.T) {
	base := []string{"line1", "line2", "line3", "line4"}

	// Ours inserts between line1 and line2
	ours := []string{"line1", "inserted_by_ours_1", "inserted_by_ours_2", "line2", "line3", "line4"}

	// Theirs inserts between line3 and line4
	theirs := []string{"line1", "line2", "line3", "inserted_by_theirs", "line4"}

	merged, clean := Diff3Merge(base, ours, theirs)
	if !clean {
		t.Fatalf("expected clean merge of non-overlapping insertions: %v", strings.Join(merged, "\n"))
	}

	expected := []string{
		"line1",
		"inserted_by_ours_1",
		"inserted_by_ours_2",
		"line2",
		"line3",
		"inserted_by_theirs",
		"line4",
	}

	if !slicesEqual(merged, expected) {
		t.Errorf("expected %v, got %v", expected, merged)
	}
}

func TestDiff3Merge_DeletionsDifferentLengths(t *testing.T) {
	base := []string{"A", "B", "C", "D", "E"}

	// Ours deletes B
	ours := []string{"A", "C", "D", "E"}

	// Theirs deletes D
	theirs := []string{"A", "B", "C", "E"}

	merged, clean := Diff3Merge(base, ours, theirs)
	if !clean {
		t.Fatalf("expected clean merge of independent deletions: %v", strings.Join(merged, "\n"))
	}

	expected := []string{"A", "C", "E"}
	if !slicesEqual(merged, expected) {
		t.Errorf("expected %v, got %v", expected, merged)
	}
}

func TestDiff3Merge_ConcurrentIdenticalEdits(t *testing.T) {
	base := []string{"import fmt", "func Main() {}"}

	// Both sides add the same import independently
	ours := []string{"import fmt", "import os", "func Main() {}"}
	theirs := []string{"import fmt", "import os", "func Main() {}"}

	merged, clean := Diff3Merge(base, ours, theirs)
	if !clean {
		t.Fatalf("expected clean merge for identical concurrent edits: %v", strings.Join(merged, "\n"))
	}

	expected := []string{"import fmt", "import os", "func Main() {}"}
	if !slicesEqual(merged, expected) {
		t.Errorf("expected %v, got %v", expected, merged)
	}
}

func TestDiff3Merge_ConflictingOverlappingEdits(t *testing.T) {
	base := []string{"timeout = 30", "retries = 3"}

	ours := []string{"timeout = 60", "retries = 3"}
	theirs := []string{"timeout = 120", "retries = 3"}

	merged, clean := Diff3Merge(base, ours, theirs)
	if clean {
		t.Fatalf("expected conflict for overlapping modifications, but got clean: %v", merged)
	}

	joined := strings.Join(merged, "\n")
	if !strings.Contains(joined, "<<<<<<< OURS (MAIN)") ||
		!strings.Contains(joined, "timeout = 60") ||
		!strings.Contains(joined, "=======") ||
		!strings.Contains(joined, "timeout = 120") ||
		!strings.Contains(joined, ">>>>>>> THEIRS (SUBMITTED)") {
		t.Fatalf("malformed conflict markers in: %s", joined)
	}
}

func TestDiff3Merge_BinaryFileDetection(t *testing.T) {
	binBase := []byte{0x00, 0x01, 0x02, 0x03}
	binOurs := []byte{0x00, 0x01, 0xFF, 0x03}
	binTheirs := []byte{0x00, 0x01, 0xAA, 0x03}

	if !isBinary(binBase) || !isBinary(binOurs) || !isBinary(binTheirs) {
		t.Fatalf("expected binary detection for null bytes")
	}

	_, clean := mergeLines(binBase, binOurs, binTheirs)
	if clean {
		t.Fatalf("expected conflict when merging differing binary files")
	}

	// Identical binary edits should merge cleanly
	mergedSame, cleanSame := mergeLines(binBase, binOurs, binOurs)
	if !cleanSame || !bytes.Equal(mergedSame, binOurs) {
		t.Fatalf("expected identical binary edits to merge cleanly")
	}
}

func TestThreeWayMerge_FullTreeIntegration(t *testing.T) {
	store, err := cas.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create CAS store: %v", err)
	}

	// Put initial blobs
	m1, _ := store.PutBlob([]byte("file1 original content\n"))
	m2, _ := store.PutBlob([]byte("file2 original content\n"))

	baseTree := tree.NewTree()
	baseTree.Entries["file1.txt"] = tree.Entry{Path: "file1.txt", Type: tree.TypeBlob, Hash: m1.BlobHash, Size: m1.TotalSize, Mode: 0644}
	baseTree.Entries["file2.txt"] = tree.Entry{Path: "file2.txt", Type: tree.TypeBlob, Hash: m2.BlobHash, Size: m2.TotalSize, Mode: 0644}
	baseTree.ComputeHash()

	// Ours modifies file1
	m1Ours, _ := store.PutBlob([]byte("file1 modified by ours\n"))
	oursTree := tree.NewTree()
	oursTree.Entries["file1.txt"] = tree.Entry{Path: "file1.txt", Type: tree.TypeBlob, Hash: m1Ours.BlobHash, Size: m1Ours.TotalSize, Mode: 0644}
	oursTree.Entries["file2.txt"] = tree.Entry{Path: "file2.txt", Type: tree.TypeBlob, Hash: m2.BlobHash, Size: m2.TotalSize, Mode: 0644}
	oursTree.ComputeHash()

	// Theirs modifies file2 and adds file3
	m2Theirs, _ := store.PutBlob([]byte("file2 modified by theirs\n"))
	m3Theirs, _ := store.PutBlob([]byte("file3 newly added by theirs\n"))
	theirsTree := tree.NewTree()
	theirsTree.Entries["file1.txt"] = tree.Entry{Path: "file1.txt", Type: tree.TypeBlob, Hash: m1.BlobHash, Size: m1.TotalSize, Mode: 0644}
	theirsTree.Entries["file2.txt"] = tree.Entry{Path: "file2.txt", Type: tree.TypeBlob, Hash: m2Theirs.BlobHash, Size: m2Theirs.TotalSize, Mode: 0644}
	theirsTree.Entries["file3.txt"] = tree.Entry{Path: "file3.txt", Type: tree.TypeBlob, Hash: m3Theirs.BlobHash, Size: m3Theirs.TotalSize, Mode: 0644}
	theirsTree.ComputeHash()

	result, err := ThreeWayMerge(baseTree, oursTree, theirsTree, store)
	if err != nil {
		t.Fatalf("ThreeWayMerge failed: %v", err)
	}
	if !result.Clean {
		t.Fatalf("expected clean merge, got %d conflicts", len(result.Conflicts))
	}

	if len(result.MergedTree.Entries) != 3 {
		t.Fatalf("expected 3 entries in merged tree, got %d", len(result.MergedTree.Entries))
	}

	// Verify file1 has ours hash
	if result.MergedTree.Entries["file1.txt"].Hash != m1Ours.BlobHash {
		t.Errorf("file1.txt expected ours hash")
	}
	// Verify file2 has theirs hash
	if result.MergedTree.Entries["file2.txt"].Hash != m2Theirs.BlobHash {
		t.Errorf("file2.txt expected theirs hash")
	}
	// Verify file3 was accepted
	if result.MergedTree.Entries["file3.txt"].Hash != m3Theirs.BlobHash {
		t.Errorf("file3.txt expected theirs hash")
	}
}

func TestMergeLines_PreservesCRLF(t *testing.T) {
	baseContent := []byte("line1\r\nline2\r\nline3\r\n")
	oursContent := []byte("line1_edited\r\nline2\r\nline3\r\n")
	theirsContent := []byte("line1\r\nline2\r\nline3_edited\r\n")

	mergedBlob, clean := mergeLines(baseContent, oursContent, theirsContent)
	if !clean {
		t.Fatalf("expected clean merge with CRLF files")
	}

	if !bytes.Contains(mergedBlob, []byte("\r\n")) {
		t.Errorf("expected merged content to preserve CRLF line endings, got: %q", string(mergedBlob))
	}
	expected := "line1_edited\r\nline2\r\nline3_edited\r\n"
	if string(mergedBlob) != expected {
		t.Errorf("unexpected content: %q (expected %q)", string(mergedBlob), expected)
	}
}

func TestThreeWayMerge_FileDirectoryCollision(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := cas.NewStore(tmpDir)

	baseTree := tree.NewTree()
	baseTree.ComputeHash()

	// Ours adds "logger" as a file
	mFile, _ := store.PutBlob([]byte("logger file content"))
	oursTree := tree.NewTree()
	oursTree.Entries["logger"] = tree.Entry{Path: "logger", Type: tree.TypeBlob, Hash: mFile.BlobHash, Size: mFile.TotalSize, Mode: 0644}
	oursTree.ComputeHash()

	// Theirs adds "logger/logger.go" as a file inside directory "logger"
	mDirFile, _ := store.PutBlob([]byte("package logger"))
	theirsTree := tree.NewTree()
	theirsTree.Entries["logger/logger.go"] = tree.Entry{Path: "logger/logger.go", Type: tree.TypeBlob, Hash: mDirFile.BlobHash, Size: mDirFile.TotalSize, Mode: 0644}
	theirsTree.ComputeHash()

	result, err := ThreeWayMerge(baseTree, oursTree, theirsTree, store)
	if err != nil {
		t.Fatalf("ThreeWayMerge failed: %v", err)
	}

	if result.Clean {
		t.Errorf("expected merge to detect file vs directory structural collision")
	}
	if len(result.Conflicts) == 0 {
		t.Errorf("expected conflicts for structural collision, got none")
	}
}


