package queue

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/tree"
)

func setupTestEnv(t *testing.T) (*db.DB, *cas.Store, string) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	store, err := cas.NewStore(filepath.Join(tempDir, "cas"))
	if err != nil {
		t.Fatalf("failed to open test CAS: %v", err)
	}

	repo, err := database.CreateRepo("test-repo")
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	return database, store, repo.ID
}

func TestMergeQueue_FastPath(t *testing.T) {
	database, store, repoID := setupTestEnv(t)
	defer database.Close()

	q := NewMergeQueue(database, store)

	branch, err := database.GetBranch(repoID, "main")
	if err != nil {
		t.Fatalf("failed to get main: %v", err)
	}

	// Create new changeset pointing to initial commit
	m, _ := store.PutBlob([]byte("hello world\n"))
	tr := tree.NewTree()
	tr.Entries["hello.txt"] = tree.Entry{Path: "hello.txt", Type: tree.TypeBlob, Hash: m.BlobHash, Size: m.TotalSize, Mode: 0644}
	tr.ComputeHash()
	_ = database.SaveTree(tr)

	cs := graph.NewChangeset(repoID, tr.RootHash, []string{branch.HeadID}, "Agent <agent@bot.ai>", "feat: initial feature", "bot-1", "", "")

	res, err := q.Submit(repoID, cs)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	if res.Status != StatusMerged {
		t.Fatalf("expected StatusMerged, got %s: %s", res.Status, res.ErrorMessage)
	}
	if res.SequenceNumber != 2 {
		t.Fatalf("expected sequence number 2, got %d", res.SequenceNumber)
	}

	// Verify main was updated
	updatedBranch, _ := database.GetBranch(repoID, "main")
	if updatedBranch.HeadID != cs.ID {
		t.Fatalf("expected head to be %s, got %s", cs.ID, updatedBranch.HeadID)
	}
	if updatedBranch.SequenceNumber != 2 {
		t.Fatalf("expected sequence 2, got %d", updatedBranch.SequenceNumber)
	}
}

func TestMergeQueue_SpeculativeRebase(t *testing.T) {
	database, store, repoID := setupTestEnv(t)
	defer database.Close()

	q := NewMergeQueue(database, store)

	branch, err := database.GetBranch(repoID, "main")
	if err != nil {
		t.Fatalf("failed to get main: %v", err)
	}

	// 1. Advance main with commit A (adds a.txt)
	mA, _ := store.PutBlob([]byte("content of a\n"))
	trA := tree.NewTree()
	trA.Entries["a.txt"] = tree.Entry{Path: "a.txt", Type: tree.TypeBlob, Hash: mA.BlobHash, Size: mA.TotalSize, Mode: 0644}
	trA.ComputeHash()
	_ = database.SaveTree(trA)
	csA := graph.NewChangeset(repoID, trA.RootHash, []string{branch.HeadID}, "Author A", "add a.txt", "", "", "")
	resA, err := q.Submit(repoID, csA)
	if err != nil || resA.Status != StatusMerged {
		t.Fatalf("failed to land commit A: %v", err)
	}

	// 2. Commit B was branched from the original root (branch.HeadID), modifying b.txt
	mB, _ := store.PutBlob([]byte("content of b\n"))
	trB := tree.NewTree()
	trB.Entries["b.txt"] = tree.Entry{Path: "b.txt", Type: tree.TypeBlob, Hash: mB.BlobHash, Size: mB.TotalSize, Mode: 0644}
	trB.ComputeHash()
	_ = database.SaveTree(trB)
	csB := graph.NewChangeset(repoID, trB.RootHash, []string{branch.HeadID}, "Author B", "add b.txt", "", "", "")

	// Submit B: requires speculative rebase onto commit A
	resB, err := q.Submit(repoID, csB)
	if err != nil {
		t.Fatalf("Submit B failed: %v", err)
	}
	if resB.Status != StatusMerged {
		t.Fatalf("expected B to be merged via speculative rebase, got %s: %s", resB.Status, resB.ErrorMessage)
	}
	if resB.SequenceNumber != 3 {
		t.Fatalf("expected sequence 3, got %d", resB.SequenceNumber)
	}

	// Verify merged tree has BOTH a.txt and b.txt
	landedCS, err := database.GetChangeset(resB.LandedCSID)
	if err != nil {
		t.Fatalf("failed to get landed changeset: %v", err)
	}
	landedTree, err := database.GetTree(landedCS.RootTreeHash)
	if err != nil {
		t.Fatalf("failed to get landed tree: %v", err)
	}
	if _, ok := landedTree.Entries["a.txt"]; !ok {
		t.Errorf("expected merged tree to contain a.txt")
	}
	if _, ok := landedTree.Entries["b.txt"]; !ok {
		t.Errorf("expected merged tree to contain b.txt")
	}
}

func TestMergeQueue_ConcurrentDistributedInstances(t *testing.T) {
	database, store, repoID := setupTestEnv(t)
	defer database.Close()

	branch, _ := database.GetBranch(repoID, "main")
	initialHead := branch.HeadID

	// Create 10 separate MergeQueue instances simulating 10 distinct servers hitting same DB
	numServers := 10
	var wg sync.WaitGroup
	results := make([]*SubmitResult, numServers)
	errors := make([]error, numServers)

	for i := 0; i < numServers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Each server has its own MergeQueue instance
			serverQueue := NewMergeQueue(database, store)

			fileName := fmt.Sprintf("service_%d.go", idx)
			blob, _ := store.PutBlob([]byte(fmt.Sprintf("package service_%d\n", idx)))
			tr := tree.NewTree()
			tr.Entries[fileName] = tree.Entry{Path: fileName, Type: tree.TypeBlob, Hash: blob.BlobHash, Size: blob.TotalSize, Mode: 0644}
			tr.ComputeHash()
			_ = database.SaveTree(tr)

			// All branched from original initialHead
			cs := graph.NewChangeset(repoID, tr.RootHash, []string{initialHead}, fmt.Sprintf("Server-%d", idx), fmt.Sprintf("feat: worker %d", idx), "", "", "")

			res, err := serverQueue.Submit(repoID, cs)
			results[idx] = res
			errors[idx] = err
		}(i)
	}

	testSQLite := func(t *testing.T) {
		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "shared.db")
		casPath := filepath.Join(tempDir, "cas")

		database1, err := db.Open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer database1.Close()
		database2, err := db.Open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer database2.Close()

		store1, err := cas.NewStore(casPath)
		if err != nil {
			t.Fatal(err)
		}
		store2, err := cas.NewStore(casPath)
		if err != nil {
			t.Fatal(err)
		}
		repo, err := database1.CreateRepo("shared-process-repo")
		if err != nil {
			t.Fatal(err)
		}
		branch, err := database1.GetBranch(repo.ID, "main")
		if err != nil {
			t.Fatal(err)
		}

		const submissions = 100
		results := make(chan *SubmitResult, submissions)
		errors := make(chan error, submissions)
		var wg sync.WaitGroup
		for i := 0; i < submissions; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				database := database1
				store := store1
				if index%2 == 1 {
					database = database2
					store = store2
				}
				blob, err := store.PutBlob([]byte(fmt.Sprintf("package worker%d\n", index)))
				if err != nil {
					errors <- err
					return
				}
				tr := tree.NewTree()
				path := fmt.Sprintf("worker%d.go", index)
				tr.Entries[path] = tree.Entry{Path: path, Type: tree.TypeBlob, Hash: blob.BlobHash, Size: blob.TotalSize, Mode: 0644}
				tr.ComputeHash()
				if err := database.SaveTree(tr); err != nil {
					errors <- err
					return
				}
				cs := graph.NewChangeset(repo.ID, tr.RootHash, []string{branch.HeadID}, fmt.Sprintf("worker-%d", index), "concurrent submission", "", "", "")
				result, err := NewMergeQueue(database, store).Submit(repo.ID, cs)
				if err != nil {
					errors <- err
					return
				}
				results <- result
			}(i)
		}
		wg.Wait()
		close(results)
		close(errors)

		for err := range errors {
			t.Fatalf("concurrent submission failed: %v", err)
		}
		merged := 0
		for result := range results {
			if result.Status != StatusMerged {
				t.Fatalf("concurrent submission returned %s: %s", result.Status, result.ErrorMessage)
			}
			merged++
		}
		if merged != submissions {
			t.Fatalf("expected %d merged submissions, got %d", submissions, merged)
		}

		finalBranch, err := database1.GetBranch(repo.ID, "main")
		if err != nil {
			t.Fatal(err)
		}
		if finalBranch.SequenceNumber != submissions+1 {
			t.Fatalf("expected final sequence %d, got %d", submissions+1, finalBranch.SequenceNumber)
		}
	}
	testSQLite(t)

	wg.Wait()

	// Verify all 10 submissions landed
	for i := 0; i < numServers; i++ {
		if errors[i] != nil {
			t.Fatalf("server %d failed with error: %v", i, errors[i])
		}
		if results[i].Status != StatusMerged {
			t.Fatalf("server %d failed to merge: %s (%s)", i, results[i].Status, results[i].ErrorMessage)
		}
	}

	// Verify branch ended up with sequence number 1 + 10 = 11
	finalBranch, _ := database.GetBranch(repoID, "main")
	if finalBranch.SequenceNumber != 11 {
		t.Fatalf("expected sequence 11, got %d", finalBranch.SequenceNumber)
	}

	// Verify final tree contains all 10 files
	tipCS, _ := database.GetChangeset(finalBranch.HeadID)
	tipTree, _ := database.GetTree(tipCS.RootTreeHash)
	for i := 0; i < numServers; i++ {
		fn := fmt.Sprintf("service_%d.go", i)
		if _, ok := tipTree.Entries[fn]; !ok {
			t.Errorf("missing file %s in final rebased tree", fn)
		}
	}
}
