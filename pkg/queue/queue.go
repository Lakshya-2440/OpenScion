package queue

import (
	"fmt"
	"sync"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/merge"
	"github.com/aether/scn/pkg/tree"
	"github.com/google/uuid"
)

type SubmitStatus string

const (
	StatusMerged         SubmitStatus = "MERGED"
	StatusRebaseConflict SubmitStatus = "REBASE_CONFLICT"
	StatusFailed         SubmitStatus = "FAILED"
)

type SubmitResult struct {
	Status         SubmitStatus     `json:"status"`
	LandedCSID     string           `json:"landed_changeset_id"`
	SequenceNumber int64            `json:"sequence_number"`
	Conflicts      []merge.Conflict `json:"conflicts,omitempty"`
	ErrorMessage   string           `json:"error_message,omitempty"`
	DurationMs     int64            `json:"duration_ms"`
}

type MergeQueue struct {
	db        *db.DB
	store     *cas.Store
	repoLocks sync.Map // repoID (string) -> *sync.Mutex
}

func NewMergeQueue(database *db.DB, store *cas.Store) *MergeQueue {
	return &MergeQueue{
		db:    database,
		store: store,
	}
}

func (q *MergeQueue) getRepoLock(repoID string) *sync.Mutex {
	val, _ := q.repoLocks.LoadOrStore(repoID, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// Submit processes a changeset submission through speculative rebase and distributed atomic CAS landing
func (q *MergeQueue) Submit(repoID string, cs *graph.Changeset) (*SubmitResult, error) {
	start := time.Now()
	if cs == nil || cs.RootTreeHash == "" {
		return nil, fmt.Errorf("changeset and root tree are required")
	}
	treeValue, err := q.db.GetTree(cs.RootTreeHash)
	if err != nil {
		return nil, fmt.Errorf("submitted tree is not available: %w", err)
	}
	if err := treeValue.Validate(); err != nil {
		return nil, fmt.Errorf("submitted tree failed validation: %w", err)
	}

	// Verify submitted CAS content (diff-only against parent or main tip to avoid multi-GB re-reads on large monorepos)
	entriesToVerify := treeValue.Entries
	if len(cs.ParentIDs) > 0 && cs.ParentIDs[0] != "" {
		if parentCS, err := q.db.GetChangeset(cs.ParentIDs[0]); err == nil && parentCS != nil {
			if parentTree, err := q.db.GetTree(parentCS.RootTreeHash); err == nil && parentTree != nil {
				diffEntries := make(map[string]tree.Entry)
				for p, entry := range treeValue.Entries {
					if parentEntry, exists := parentTree.Entries[p]; !exists || parentEntry.Hash != entry.Hash {
						diffEntries[p] = entry
					}
				}
				entriesToVerify = diffEntries
			}
		}
	}
	// Fallback to diffing against tip of main if parent commit was speculatively rebased by server
	if len(entriesToVerify) == len(treeValue.Entries) {
		if mainBranch, err := q.db.GetBranch(repoID, "main"); err == nil && mainBranch != nil {
			if tipCS, err := q.db.GetChangeset(mainBranch.HeadID); err == nil && tipCS != nil {
				if tipTree, err := q.db.GetTree(tipCS.RootTreeHash); err == nil && tipTree != nil {
					diffEntries := make(map[string]tree.Entry)
					for p, entry := range treeValue.Entries {
						if tipEntry, exists := tipTree.Entries[p]; !exists || tipEntry.Hash != entry.Hash {
							diffEntries[p] = entry
						}
					}
					entriesToVerify = diffEntries
				}
			}
		}
	}

	// Verify submitted CAS content concurrently via fast HasManifest check
	if len(entriesToVerify) > 0 {
		var missingErr error
		var once sync.Once
		workChan := make(chan tree.Entry, len(entriesToVerify))
		for _, entry := range entriesToVerify {
			workChan <- entry
		}
		close(workChan)

		numWorkers := 16
		if len(entriesToVerify) < numWorkers {
			numWorkers = len(entriesToVerify)
		}

		var wg sync.WaitGroup
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for entry := range workChan {
					if missingErr != nil {
						return
					}
					if !q.store.HasManifest(entry.Hash) {
						once.Do(func() {
							missingErr = fmt.Errorf("submitted content for %s is unavailable", entry.Path)
						})
						return
					}
				}
			}()
		}
		wg.Wait()
		if missingErr != nil {
			return nil, missingErr
		}
	}

	repoLock := q.getRepoLock(repoID)
	repoLock.Lock()
	defer repoLock.Unlock()

	cs.RepoID = repoID
	cs.ID = cs.ComputeID()
	queueID := uuid.New().String()
	var initialParent string
	if len(cs.ParentIDs) > 0 {
		initialParent = cs.ParentIDs[0]
	}
	if err := q.db.EnqueueMerge(queueID, repoID, cs.ID, initialParent); err != nil {
		return nil, fmt.Errorf("failed to enqueue changeset: %w", err)
	}

	const maxRetries = 50
	var lastConflicts []merge.Conflict

	for attempt := 0; attempt < maxRetries; attempt++ {
		// 1. Fetch current tip of 'main'
		mainBranch, err := q.db.GetBranch(repoID, "main")
		if err != nil {
			_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
			return nil, fmt.Errorf("failed to fetch main branch: %w", err)
		}

		currentHeadCS, err := q.db.GetChangeset(mainBranch.HeadID)
		if err != nil {
			_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
			return nil, fmt.Errorf("failed to fetch current head commit %s: %w", mainBranch.HeadID, err)
		}

		expectedHeadID := mainBranch.HeadID
		expectedSeq := mainBranch.SequenceNumber
		nextSeq := expectedSeq + 1

		// Fast Path: Changeset was built directly on current head
		if len(cs.ParentIDs) > 0 && cs.ParentIDs[0] == currentHeadCS.ID {
			cs.SequenceNumber = nextSeq
			if err := q.db.SaveChangeset(cs); err != nil {
				_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
				return nil, fmt.Errorf("failed to save landed changeset: %w", err)
			}

			// Atomic CAS branch tip update across distributed server nodes
			ok, err := q.db.CompareAndSwapBranchTip(repoID, "main", expectedHeadID, expectedSeq, cs.ID, nextSeq)
			if err != nil {
				_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
				return nil, fmt.Errorf("failed to execute branch CAS update: %w", err)
			}
			if ok {
				if err := q.db.UpdateMergeQueueStatus(queueID, "MERGED", time.Now().Unix()); err != nil {
					return nil, fmt.Errorf("changeset landed but queue status update failed: %w", err)
				}
				return &SubmitResult{
					Status:         StatusMerged,
					LandedCSID:     cs.ID,
					SequenceNumber: nextSeq,
					DurationMs:     time.Since(start).Milliseconds(),
				}, nil
			}

			// CAS conflict: another server instance advanced main, retry with speculative rebase
			delay := 10 * time.Millisecond * time.Duration(attempt+1)
			if delay > 250*time.Millisecond {
				delay = 250 * time.Millisecond
			}
			time.Sleep(delay)
			continue
		}

		// Rebase Path: Head of main has advanced since agent checkout
		var baseTreeHash string
		if len(cs.ParentIDs) > 0 {
			baseCS, err := q.db.GetChangeset(cs.ParentIDs[0])
			if err == nil {
				baseTreeHash = baseCS.RootTreeHash
			}
		}

		baseTree, err := q.db.GetTree(baseTreeHash)
		if err != nil {
			baseTree, _ = q.db.GetTree(currentHeadCS.RootTreeHash)
		}
		oursTree, err := q.db.GetTree(currentHeadCS.RootTreeHash)
		if err != nil {
			_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
			return nil, fmt.Errorf("failed to load current head tree: %w", err)
		}
		theirsTree, err := q.db.GetTree(cs.RootTreeHash)
		if err != nil {
			_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
			return nil, fmt.Errorf("failed to load submitted tree: %w", err)
		}

		// Perform 3-way speculative rebase
		mergeResult, err := merge.ThreeWayMerge(baseTree, oursTree, theirsTree, q.store)
		if err != nil {
			_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
			return nil, fmt.Errorf("merge engine error: %w", err)
		}

		if !mergeResult.Clean {
			lastConflicts = mergeResult.Conflicts
			_ = q.db.UpdateMergeQueueStatus(queueID, "REBASE_CONFLICT", 0)
			return &SubmitResult{
				Status:       StatusRebaseConflict,
				Conflicts:    lastConflicts,
				ErrorMessage: fmt.Sprintf("Rebase failed: %d conflicting file(s)", len(lastConflicts)),
				DurationMs:   time.Since(start).Milliseconds(),
			}, nil
		}

		// Clean rebase succeeded: save merged tree
		if err := q.db.SaveTree(mergeResult.MergedTree); err != nil {
			_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
			return nil, fmt.Errorf("failed to save rebased tree: %w", err)
		}

		rebasedCS := graph.NewChangeset(
			repoID,
			mergeResult.MergedTree.RootHash,
			[]string{currentHeadCS.ID},
			cs.Author,
			cs.Message,
			cs.AgentModel,
			cs.SessionID,
			cs.TaskTraceID,
		)
		rebasedCS.SequenceNumber = nextSeq

		if err := q.db.SaveChangeset(rebasedCS); err != nil {
			_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
			return nil, fmt.Errorf("failed to save rebased changeset: %w", err)
		}

		// Atomic CAS branch tip update across distributed server nodes
		ok, err := q.db.CompareAndSwapBranchTip(repoID, "main", expectedHeadID, expectedSeq, rebasedCS.ID, nextSeq)
		if err != nil {
			_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
			return nil, fmt.Errorf("failed to execute branch CAS update: %w", err)
		}
		if ok {
			if err := q.db.UpdateMergeQueueStatus(queueID, "MERGED", time.Now().Unix()); err != nil {
				return nil, fmt.Errorf("changeset landed but queue status update failed: %w", err)
			}
			return &SubmitResult{
				Status:         StatusMerged,
				LandedCSID:     rebasedCS.ID,
				SequenceNumber: nextSeq,
				DurationMs:     time.Since(start).Milliseconds(),
			}, nil
		}

		// CAS conflict: another server node landed concurrently, backoff & retry onto the latest tip
		delay := 10 * time.Millisecond * time.Duration(attempt+1)
		if delay > 250*time.Millisecond {
			delay = 250 * time.Millisecond
		}
		time.Sleep(delay)
	}

	_ = q.db.UpdateMergeQueueStatus(queueID, "FAILED", 0)
	return &SubmitResult{
		Status:       StatusFailed,
		Conflicts:    lastConflicts,
		ErrorMessage: fmt.Sprintf("Exceeded max speculative rebase retries (%d) under concurrent distributed load", maxRetries),
		DurationMs:   time.Since(start).Milliseconds(),
	}, nil
}
