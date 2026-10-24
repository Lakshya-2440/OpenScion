package tree

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aether/scn/pkg/cas"
)

func TestMatchesAnyPattern_NoPrefixFalsePositives(t *testing.T) {
	patterns := []string{
		".scion/**", "mnt/**", "mnt", "node_modules/**", "dist/**", "bin/**",
		"*.db", ".DS_Store",
	}

	// Should match exact or directory contents
	if !MatchesAnyPattern("mnt", patterns) {
		t.Errorf("expected mnt to match pattern mnt")
	}
	if !MatchesAnyPattern("mnt/cache.txt", patterns) {
		t.Errorf("expected mnt/cache.txt to match pattern mnt/**")
	}
	if !MatchesAnyPattern(".scion/config.json", patterns) {
		t.Errorf("expected .scion/config.json to match pattern .scion/**")
	}
	if !MatchesAnyPattern("sub/folder/file.db", patterns) {
		t.Errorf("expected sub/folder/file.db to match *.db")
	}

	// CRITICAL: should NOT match files that simply share a prefix
	if MatchesAnyPattern("mnt_engine.go", patterns) {
		t.Errorf("BUG: mnt_engine.go matched pattern 'mnt' (prefix false positive)")
	}
	if MatchesAnyPattern("binary_search.go", patterns) {
		t.Errorf("BUG: binary_search.go matched pattern 'bin/**'")
	}
	if MatchesAnyPattern("distribute.go", patterns) {
		t.Errorf("BUG: distribute.go matched pattern 'dist/**'")
	}
}

func TestBuildTreeFromDirWithBase_StatCache(t *testing.T) {
	tmpDir := t.TempDir()
	casDir := filepath.Join(tmpDir, ".scion", "cas")
	store, err := cas.NewStore(casDir)
	if err != nil {
		t.Fatalf("failed to create CAS: %v", err)
	}

	// Create test file
	filePath := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("Hello OpenScion!"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Initial tree build
	tree1, err := BuildTreeFromDirWithBase(tmpDir, store, nil, nil)
	if err != nil {
		t.Fatalf("initial build failed: %v", err)
	}

	entry1, ok := tree1.Entries["hello.txt"]
	if !ok {
		t.Fatalf("hello.txt not found in tree")
	}

	// Build again with tree1 as base - should hit cache
	tree2, err := BuildTreeFromDirWithBase(tmpDir, store, nil, tree1)
	if err != nil {
		t.Fatalf("cached build failed: %v", err)
	}

	entry2, ok := tree2.Entries["hello.txt"]
	if !ok {
		t.Fatalf("hello.txt not found in cached tree")
	}

	if entry1.Hash != entry2.Hash {
		t.Errorf("hash mismatch: %s vs %s", entry1.Hash, entry2.Hash)
	}
	if tree1.RootHash != tree2.RootHash {
		t.Errorf("root hash mismatch: %s vs %s", tree1.RootHash, tree2.RootHash)
	}

	// Modify file content & modtime
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("Hello OpenScion Modified!"), 0644); err != nil {
		t.Fatalf("failed to update file: %v", err)
	}

	tree3, err := BuildTreeFromDirWithBase(tmpDir, store, nil, tree2)
	if err != nil {
		t.Fatalf("modified build failed: %v", err)
	}

	entry3 := tree3.Entries["hello.txt"]
	if entry3.Hash == entry2.Hash {
		t.Errorf("expected hash change after file modification")
	}
}

func TestBuildTreeFromDirWithBase_BrokenSymlinkAndSpecialFiles(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := cas.NewStore(filepath.Join(tmpDir, ".cas"))

	// Create regular file
	_ = os.WriteFile(filepath.Join(tmpDir, "valid.txt"), []byte("valid content"), 0644)

	// Create broken symlink pointing to non-existent file
	_ = os.Symlink(filepath.Join(tmpDir, "non_existent.txt"), filepath.Join(tmpDir, "broken_link.txt"))

	// Build tree - should succeed without error or crash
	tr, err := BuildTreeFromDirWithBase(tmpDir, store, nil, nil)
	if err != nil {
		t.Fatalf("BuildTreeFromDirWithBase should not fail on broken symlink: %v", err)
	}

	if _, ok := tr.Entries["valid.txt"]; !ok {
		t.Errorf("valid.txt missing from tree")
	}
}

func TestBuildTreeFromDirWithBaseAndSparse_PreservesUnmanagedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := cas.NewStore(filepath.Join(tmpDir, ".cas"))

	// Create base tree representing full monorepo
	baseTree := NewTree()
	baseTree.Entries["services/auth/main.go"] = Entry{
		Path: "services/auth/main.go",
		Type: TypeBlob,
		Hash: "hash-auth-1",
		Size: 100,
		Mode: 0644,
	}
	baseTree.Entries["services/billing/main.go"] = Entry{
		Path: "services/billing/main.go",
		Type: TypeBlob,
		Hash: "hash-billing-1",
		Size: 200,
		Mode: 0644,
	}
	baseTree.ComputeHash()

	// In sparse workspace, only services/auth is on disk
	authDir := filepath.Join(tmpDir, "services", "auth")
	_ = os.MkdirAll(authDir, 0755)
	_ = os.WriteFile(filepath.Join(authDir, "main.go"), []byte("package main // modified auth"), 0644)

	// Build tree with sparse profile "services/auth/**"
	sparseTree, err := BuildTreeFromDirWithBaseAndSparse(tmpDir, store, nil, baseTree, []string{"services/auth/**"})
	if err != nil {
		t.Fatalf("sparse tree build failed: %v", err)
	}

	// billing/main.go must be preserved from baseTree even though not on disk!
	billingEntry, exists := sparseTree.Entries["services/billing/main.go"]
	if !exists {
		t.Fatalf("CRITICAL BUG: services/billing/main.go was dropped during sparse commit!")
	}
	if billingEntry.Hash != "hash-billing-1" {
		t.Errorf("expected preserved billing hash 'hash-billing-1', got %s", billingEntry.Hash)
	}

	// auth/main.go should reflect new on-disk version
	authEntry, exists := sparseTree.Entries["services/auth/main.go"]
	if !exists {
		t.Fatalf("services/auth/main.go missing from sparse tree")
	}
	if authEntry.Hash == "hash-auth-1" {
		t.Errorf("expected new hash for modified auth file")
	}
}

func TestBuildTreeFromDirWithBase_CaseCollision(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := cas.NewStore(filepath.Join(tmpDir, ".cas"))

	// Simulate collision: on case-sensitive or test setup
	// We test caseMap by calling BuildTree
	_ = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("content 1"), 0644)
	
	tr, err := BuildTreeFromDirWithBase(tmpDir, store, nil, nil)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, ok := tr.Entries["test.txt"]; !ok {
		t.Errorf("test.txt missing")
	}
}

