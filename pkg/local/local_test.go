package local

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aether/scn/pkg/api"
	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
)

func TestMaterializeTreeDelta_DeletesRemovedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := InitLocalRepo(tmpDir, "test-repo")
	if err != nil {
		t.Fatalf("InitLocalRepo failed: %v", err)
	}
	defer repo.Close()

	// 1. Create files A and B
	fileA := filepath.Join(tmpDir, "fileA.txt")
	fileB := filepath.Join(tmpDir, "fileB.txt")
	_ = os.WriteFile(fileA, []byte("Content A"), 0644)
	_ = os.WriteFile(fileB, []byte("Content B"), 0644)

	cs1, err := repo.Commit("commit 1", "author", "", "", "")
	if err != nil {
		t.Fatalf("commit 1 failed: %v", err)
	}
	tree1, _ := repo.DB.GetTree(cs1.RootTreeHash)

	// 2. Remove fileB from disk and commit
	_ = os.Remove(fileB)
	cs2, err := repo.Commit("commit 2", "author", "", "", "")
	if err != nil {
		t.Fatalf("commit 2 failed: %v", err)
	}
	tree2, _ := repo.DB.GetTree(cs2.RootTreeHash)

	// 3. Put fileB back manually (simulating dirty/unpulled state)
	_ = os.WriteFile(fileB, []byte("Zombie file"), 0644)

	// Materialize tree2 against tree1 as delta
	updated := repo.MaterializeTreeDelta(tree1, tree2)
	_ = updated

	// Verify fileB was deleted from disk
	if _, err := os.Stat(fileB); !os.IsNotExist(err) {
		t.Errorf("BUG: fileB.txt should have been deleted by MaterializeTreeDelta")
	}
}

func TestWorkspaceLock(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := InitLocalRepo(tmpDir, "lock-repo")
	if err != nil {
		t.Fatalf("InitLocalRepo failed: %v", err)
	}
	defer repo.Close()

	// Acquire lock
	unlock1, err := repo.AcquireLock("test-op")
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}

	// Try acquiring again - should fail
	_, err = repo.AcquireLock("concurrent-op")
	if err == nil {
		t.Fatalf("expected second AcquireLock to fail with concurrency error")
	}

	// Release lock
	unlock1()

	// Now re-acquiring should succeed
	unlock2, err := repo.AcquireLock("second-op")
	if err != nil {
		t.Fatalf("re-acquiring lock failed: %v", err)
	}
	unlock2()
}

func TestHistoricalBlame(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := InitLocalRepo(tmpDir, "blame-repo")
	if err != nil {
		t.Fatalf("InitLocalRepo failed: %v", err)
	}
	defer repo.Close()

	testFile := filepath.Join(tmpDir, "code.txt")

	// Commit 1: Add lines 1 and 2
	_ = os.WriteFile(testFile, []byte("line 1\nline 2"), 0644)
	cs1, err := repo.Commit("commit 1", "Alice", "", "", "")
	if err != nil {
		t.Fatalf("commit 1 failed: %v", err)
	}

	// Commit 2: Add line 3
	_ = os.WriteFile(testFile, []byte("line 1\nline 2\nline 3"), 0644)
	cs2, err := repo.Commit("commit 2", "Bob", "", "", "")
	if err != nil {
		t.Fatalf("commit 2 failed: %v", err)
	}

	// Run Blame
	blameLines, err := repo.Blame("code.txt")
	if err != nil {
		t.Fatalf("blame failed: %v", err)
	}

	if len(blameLines) != 3 {
		t.Fatalf("expected 3 blame lines, got %d", len(blameLines))
	}

	// Verify line 1 is attributed to Alice (commit 1)
	if !strings.Contains(blameLines[0], "Alice") || !strings.Contains(blameLines[0], cs1.ShortID()) {
		t.Errorf("line 1 attribution incorrect: %s (expected Alice / %s)", blameLines[0], cs1.ShortID())
	}

	// Verify line 3 is attributed to Bob (commit 2)
	if !strings.Contains(blameLines[2], "Bob") || !strings.Contains(blameLines[2], cs2.ShortID()) {
		t.Errorf("line 3 attribution incorrect: %s (expected Bob / %s)", blameLines[2], cs2.ShortID())
	}
}

func TestBranchAndSwitch(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := InitLocalRepo(tmpDir, "branch-repo")
	if err != nil {
		t.Fatalf("InitLocalRepo failed: %v", err)
	}
	defer repo.Close()

	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("main v1"), 0644)
	csMain1, err := repo.Commit("main initial", "Alice", "", "", "")
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// 1. Create branch "feat-x"
	if err := repo.CreateBranch("feat-x"); err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	branches, err := repo.ListBranches()
	if err != nil {
		t.Fatalf("ListBranches failed: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(branches))
	}

	// 2. Switch to feat-x
	if err := repo.SwitchBranch("feat-x"); err != nil {
		t.Fatalf("SwitchBranch failed: %v", err)
	}
	if repo.Config.Branch != "feat-x" {
		t.Errorf("expected current branch feat-x, got %s", repo.Config.Branch)
	}

	// 3. Commit on feat-x
	_ = os.WriteFile(testFile, []byte("feat-x v2"), 0644)
	csFeat, err := repo.Commit("feat-x change", "Bob", "", "", "")
	if err != nil {
		t.Fatalf("commit on feat-x failed: %v", err)
	}

	// 4. Switch back to main - workspace should update to main v1
	if err := repo.SwitchBranch("main"); err != nil {
		t.Fatalf("SwitchBranch back to main failed: %v", err)
	}
	content, _ := os.ReadFile(testFile)
	if string(content) != "main v1" {
		t.Errorf("expected 'main v1' after switching to main, got %q", string(content))
	}

	// 5. Switch back to feat-x - workspace should have feat-x v2
	if err := repo.SwitchBranch("feat-x"); err != nil {
		t.Fatalf("SwitchBranch to feat-x failed: %v", err)
	}
	content, _ = os.ReadFile(testFile)
	if string(content) != "feat-x v2" {
		t.Errorf("expected 'feat-x v2' after switching to feat-x, got %q", string(content))
	}

	// 6. Test DeleteBranch
	if err := repo.DeleteBranch("feat-x"); err == nil {
		t.Errorf("expected error when deleting active branch feat-x")
	}
	if err := repo.SwitchBranch("main"); err != nil {
		t.Fatalf("SwitchBranch back to main failed: %v", err)
	}
	if err := repo.DeleteBranch("main"); err == nil {
		t.Errorf("expected error when deleting default branch main")
	}
	if err := repo.DeleteBranch("feat-x"); err != nil {
		t.Fatalf("DeleteBranch feat-x failed: %v", err)
	}
	branchesAfter, _ := repo.ListBranches()
	if len(branchesAfter) != 1 {
		t.Errorf("expected 1 branch after deletion, got %d", len(branchesAfter))
	}
	_ = csMain1
	_ = csFeat
}

func TestRestoreFileAndAll(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := InitLocalRepo(tmpDir, "restore-repo")
	if err != nil {
		t.Fatalf("InitLocalRepo failed: %v", err)
	}
	defer repo.Close()

	f1 := filepath.Join(tmpDir, "file1.txt")
	f2 := filepath.Join(tmpDir, "file2.txt")
	_ = os.WriteFile(f1, []byte("original 1"), 0644)
	_ = os.WriteFile(f2, []byte("original 2"), 0644)
	if _, err := repo.Commit("init", "Alice", "", "", ""); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// Modify file1
	_ = os.WriteFile(f1, []byte("dirty 1"), 0644)
	if err := repo.RestoreFile("file1.txt", ""); err != nil {
		t.Fatalf("RestoreFile failed: %v", err)
	}
	c1, _ := os.ReadFile(f1)
	if string(c1) != "original 1" {
		t.Errorf("RestoreFile did not revert content: got %q", string(c1))
	}

	// Modify both
	_ = os.WriteFile(f1, []byte("dirty 1 again"), 0644)
	_ = os.WriteFile(f2, []byte("dirty 2 again"), 0644)
	if _, err := repo.RestoreAll(""); err != nil {
		t.Fatalf("RestoreAll failed: %v", err)
	}
	c1, _ = os.ReadFile(f1)
	c2, _ := os.ReadFile(f2)
	if string(c1) != "original 1" || string(c2) != "original 2" {
		t.Errorf("RestoreAll did not revert contents: %q, %q", string(c1), string(c2))
	}
}

func TestRepo_BranchAncestryLog(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := InitLocalRepo(tmpDir, "ancestry-repo")
	if err != nil {
		t.Fatalf("InitLocalRepo failed: %v", err)
	}
	defer repo.Close()

	// Initial commit on main
	_ = os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	_, _ = repo.Commit("commit a on main", "Author", "", "", "")

	// Create and switch to feat-branch
	_ = repo.CreateBranch("feat-branch")
	_ = repo.SwitchBranch("feat-branch")
	_ = os.WriteFile(filepath.Join(tmpDir, "feat.txt"), []byte("feat"), 0644)
	csFeat, _ := repo.Commit("commit feat", "Author", "", "", "")

	// Switch back to main and commit something else
	_ = repo.SwitchBranch("main")
	_ = os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)
	csMain, _ := repo.Commit("commit b on main", "Author", "", "", "")

	// Log on main should NOT include csFeat
	mainLog, err := repo.Log(10)
	if err != nil {
		t.Fatalf("repo.Log failed: %v", err)
	}
	for _, cs := range mainLog {
		if cs.ID == csFeat.ID {
			t.Errorf("main log should not contain commit from feat-branch: %s", cs.ID)
		}
	}
	if mainLog[0].ID != csMain.ID {
		t.Errorf("main log HEAD should be csMain, got %s", mainLog[0].ID)
	}
}

func TestRepo_GC(t *testing.T) {
	tmpDir := t.TempDir()
	repo, err := InitLocalRepo(tmpDir, "gc-repo")
	if err != nil {
		t.Fatalf("InitLocalRepo failed: %v", err)
	}
	defer repo.Close()

	// Create and commit file1
	f1 := filepath.Join(tmpDir, "file1.txt")
	_ = os.WriteFile(f1, []byte("data 1"), 0644)
	_, _ = repo.Commit("add file1", "Author", "", "", "")

	// Add an unreferenced chunk directly to CAS store
	orphanData := []byte("orphan unreferenced content")
	orphanChunk, _ := repo.Store.PutBlob(orphanData)

	// Verify orphan chunk exists
	if _, err := repo.Store.GetManifest(orphanChunk.BlobHash); err != nil {
		t.Fatalf("orphan manifest should exist prior to GC")
	}

	// Run GC
	deleted, freed, err := repo.GC()
	if err != nil {
		t.Fatalf("repo.GC failed: %v", err)
	}
	if deleted == 0 || freed == 0 {
		t.Errorf("expected GC to delete unreferenced objects, deleted: %d, freed: %d", deleted, freed)
	}

	// Orphan manifest should now be deleted
	if _, err := repo.Store.GetManifest(orphanChunk.BlobHash); err == nil {
		t.Errorf("orphan manifest should have been deleted by GC")
	}
}

func TestSync_PushAndPullProtectedRebase(t *testing.T) {
	// 1. Setup mock remote server
	serverDir := t.TempDir()
	serverDB, err := db.Open(filepath.Join(serverDir, "server.db"))
	if err != nil {
		t.Fatalf("failed to init server db: %v", err)
	}
	defer serverDB.Close()

	serverCAS, err := cas.NewStore(filepath.Join(serverDir, "cas"))
	if err != nil {
		t.Fatalf("failed to init server cas: %v", err)
	}

	server := api.NewServer(serverDB, serverCAS)
	ts := httptest.NewServer(server)
	defer ts.Close()

	// 2. Initialize Repo 1, commit, and push
	dir1 := t.TempDir()
	repo1, err := InitLocalRepo(dir1, "monorepo")
	if err != nil {
		t.Fatalf("repo1 init failed: %v", err)
	}
	defer repo1.Close()
	_ = repo1.SetRemote(ts.URL)

	f1 := filepath.Join(dir1, "file1.txt")
	_ = os.WriteFile(f1, []byte("version 1"), 0644)
	if _, err := repo1.Commit("init commit", "Dev1", "", "", ""); err != nil {
		t.Fatalf("repo1 commit failed: %v", err)
	}

	pushRes1, err := repo1.Push()
	if err != nil {
		t.Fatalf("repo1 push failed: %v", err)
	}
	if pushRes1.PushedChangesets < 1 {
		t.Errorf("expected pushed changesets, got %d", pushRes1.PushedChangesets)
	}

	// 3. Initialize Repo 2, pull
	dir2 := t.TempDir()
	repo2, err := InitLocalRepo(dir2, "monorepo")
	if err != nil {
		t.Fatalf("repo2 init failed: %v", err)
	}
	defer repo2.Close()
	_ = repo2.SetRemote(ts.URL)

	_ = os.Remove(filepath.Join(dir2, "AGENTS.md"))
	_ = os.Remove(filepath.Join(dir2, "CLAUDE.md"))

	pullRes2, err := repo2.Pull()
	if err != nil {
		t.Fatalf("repo2 initial pull failed: %v", err)
	}
	if pullRes2.PulledChangesets < 1 {
		t.Errorf("expected pulled changesets, got %d", pullRes2.PulledChangesets)
	}

	// 4. Repo 1 updates file1.txt, commits, and pushes
	_ = os.WriteFile(f1, []byte("version 1 - modified by dev 1"), 0644)
	if _, err := repo1.Commit("update file1", "Dev1", "", "", ""); err != nil {
		t.Fatalf("repo1 second commit failed: %v", err)
	}
	if _, err := repo1.Push(); err != nil {
		t.Fatalf("repo1 second push failed: %v", err)
	}

	// 5. Repo 2 independently creates file2.txt and commits LOCALLY (diverged with unpushed commits)
	f2 := filepath.Join(dir2, "file2.txt")
	_ = os.WriteFile(f2, []byte("new file by dev 2"), 0644)
	csLocal2, err := repo2.Commit("local unpushed commit by dev 2", "Dev2", "", "", "")
	if err != nil {
		t.Fatalf("repo2 local commit failed: %v", err)
	}

	// 6. Repo 2 runs Pull() -> MUST NOT WIPE csLocal2 or file2.txt!
	pullResDiverged, err := repo2.Pull()
	if err != nil {
		t.Fatalf("repo2 diverged pull failed: %v", err)
	}
	if pullResDiverged.PulledChangesets < 1 {
		t.Errorf("expected pulled changesets, got %d", pullResDiverged.PulledChangesets)
	}

	// Verify both file1 and file2 exist in Repo 2!
	c1After, err1 := os.ReadFile(filepath.Join(dir2, "file1.txt"))
	c2After, err2 := os.ReadFile(filepath.Join(dir2, "file2.txt"))
	if err1 != nil || string(c1After) != "version 1 - modified by dev 1" {
		t.Errorf("file1.txt in repo2 corrupted or not updated: %v, %s", err1, string(c1After))
	}
	if err2 != nil || string(c2After) != "new file by dev 2" {
		t.Errorf("CRITICAL DATA LOSS: file2.txt was deleted during pull! %v", err2)
	}

	// 7. Repo 2 pushes the merged work back to remote
	pushRes2, err := repo2.Push()
	if err != nil {
		t.Fatalf("repo2 push of merged work failed: %v", err)
	}
	if pushRes2.PushedChangesets < 1 {
		t.Errorf("expected pushed changesets, got %d", pushRes2.PushedChangesets)
	}

	// 8. Repo 1 pulls and receives dev 2's file2.txt
	if _, err := repo1.Pull(); err != nil {
		t.Fatalf("repo1 final pull failed: %v", err)
	}
	c2Repo1, err := os.ReadFile(filepath.Join(dir1, "file2.txt"))
	if err != nil || string(c2Repo1) != "new file by dev 2" {
		t.Errorf("repo1 missing file2.txt from repo2: %v, %s", err, string(c2Repo1))
	}
	_ = csLocal2
}

