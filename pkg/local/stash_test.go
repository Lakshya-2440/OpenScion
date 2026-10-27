package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStashWorkflow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "scion-stash-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repo, err := InitLocalRepo(tempDir, "stash-test")
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}
	defer repo.Close()

	// Initial commit
	testFile := filepath.Join(tempDir, "file1.txt")
	os.WriteFile(testFile, []byte("initial line 1\n"), 0644)
	cs1, err := repo.Commit("Initial commit", "author <a@b.com>", "", "", "")
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// 1. Stash on clean tree should fail
	_, err = repo.SaveStash("clean test")
	if err == nil {
		t.Fatalf("expected error stashing clean tree, got nil")
	}

	// 2. Modify file and stash
	os.WriteFile(testFile, []byte("modified line 1\n"), 0644)
	stash, err := repo.SaveStash("my work in progress")
	if err != nil {
		t.Fatalf("failed to save stash: %v", err)
	}
	if stash.BaseChangesetID != cs1.ID {
		t.Errorf("expected base %s, got %s", cs1.ID, stash.BaseChangesetID)
	}

	// Working tree should be restored to initial state
	data, err := os.ReadFile(testFile)
	if err != nil || string(data) != "initial line 1\n" {
		t.Fatalf("working tree not restored after stash: %s", string(data))
	}

	// 3. List stashes
	stashes, err := repo.ListStashes()
	if err != nil || len(stashes) != 1 {
		t.Fatalf("expected 1 stash, got %d", len(stashes))
	}
	if stashes[0].Message != "my work in progress" {
		t.Errorf("unexpected message %s", stashes[0].Message)
	}

	// 4. Show stash diff
	diffs, err := repo.ShowStash(0)
	if err != nil || len(diffs) != 1 {
		t.Fatalf("expected 1 diff in stash, got %d", len(diffs))
	}

	// 5. Pop stash
	popped, err := repo.PopStash(0)
	if err != nil {
		t.Fatalf("failed to pop stash: %v", err)
	}
	if popped.ID != stash.ID {
		t.Errorf("popped stash mismatch")
	}

	// Verify working tree has modified content again
	dataAfterPop, err := os.ReadFile(testFile)
	if err != nil || string(dataAfterPop) != "modified line 1\n" {
		t.Fatalf("working tree not restored to modified state after pop: %s", string(dataAfterPop))
	}

	// Stash list should now be empty
	stashesAfterPop, err := repo.ListStashes()
	if err != nil || len(stashesAfterPop) != 0 {
		t.Fatalf("expected 0 stashes after pop, got %d", len(stashesAfterPop))
	}
}
