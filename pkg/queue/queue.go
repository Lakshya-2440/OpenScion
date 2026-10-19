package queue

import (
	"fmt"
	"sync"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/merge"
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
	db    *db.DB
	store *cas.Store
	mu    sync.Mutex // Serializes landing on main to guarantee strictly linear timeline
}

func NewMergeQueue(database *db.DB, store *cas.Store) *MergeQueue {
	return &MergeQueue{
		db:    database,
		store: store,
	}
}

// Submit processes a changeset submission through the speculative rebase and linear landing pipeline
func (q *MergeQueue) Submit(repoID string, cs *graph.Changeset) (*SubmitResult, error) {
	start := time.Now()
	q.mu.Lock()
	defer q.mu.Unlock()

	// 1. Fetch current tip of 'main'
	mainBranch, err := q.db.GetBranch(repoID, "main")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch main branch: %w", err)
	}

	currentHeadCS, err := q.db.GetChangeset(mainBranch.HeadID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current head commit %s: %w", mainBranch.HeadID, err)
	}

	nextSeq := mainBranch.SequenceNumber + 1

	// Fast Path: Changeset was built directly on current head
	if len(cs.ParentIDs) > 0 && cs.ParentIDs[0] == currentHeadCS.ID {
		cs.SequenceNumber = nextSeq
		if err := q.db.SaveChangeset(cs); err != nil {
			return nil, fmt.Errorf("failed to save landed changeset: %w", err)
		}
		if err := q.db.UpdateBranchTip(repoID, "main", cs.ID, nextSeq); err != nil {
			return nil, fmt.Errorf("failed to update main tip: %w", err)
		}

		return &SubmitResult{
			Status:         StatusMerged,
			LandedCSID:     cs.ID,
			SequenceNumber: nextSeq,
			DurationMs:     time.Since(start).Milliseconds(),
		}, nil
	}

	// Rebase Path: Head of main has advanced since agent checked out base
	var baseTreeHash string
	if len(cs.ParentIDs) > 0 {
		baseCS, err := q.db.GetChangeset(cs.ParentIDs[0])
		if err == nil {
			baseTreeHash = baseCS.RootTreeHash
		}
	}

	// Fetch trees
	baseTree, err := q.db.GetTree(baseTreeHash)
	if err != nil {
		// If base tree not found, fallback to empty tree
		baseTree, _ = q.db.GetTree(currentHeadCS.RootTreeHash)
	}
	oursTree, err := q.db.GetTree(currentHeadCS.RootTreeHash)
	if err != nil {
		return nil, fmt.Errorf("failed to load current head tree: %w", err)
	}
	theirsTree, err := q.db.GetTree(cs.RootTreeHash)
	if err != nil {
		return nil, fmt.Errorf("failed to load submitted tree: %w", err)
	}

	// Perform 3-way speculative rebase
	mergeResult, err := merge.ThreeWayMerge(baseTree, oursTree, theirsTree, q.store)
	if err != nil {
		return nil, fmt.Errorf("merge engine error: %w", err)
	}

	if !mergeResult.Clean {
		return &SubmitResult{
			Status:       StatusRebaseConflict,
			Conflicts:    mergeResult.Conflicts,
			ErrorMessage: fmt.Sprintf("Rebase failed: %d conflicting file(s)", len(mergeResult.Conflicts)),
			DurationMs:   time.Since(start).Milliseconds(),
		}, nil
	}

	// Clean rebase succeeded: save merged tree
	if err := q.db.SaveTree(mergeResult.MergedTree); err != nil {
		return nil, fmt.Errorf("failed to save rebased tree: %w", err)
	}

	// Create rebased changeset with parent pointing to current head
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
		return nil, fmt.Errorf("failed to save rebased changeset: %w", err)
	}

	if err := q.db.UpdateBranchTip(repoID, "main", rebasedCS.ID, nextSeq); err != nil {
		return nil, fmt.Errorf("failed to update main branch: %w", err)
	}

	return &SubmitResult{
		Status:         StatusMerged,
		LandedCSID:     rebasedCS.ID,
		SequenceNumber: nextSeq,
		DurationMs:     time.Since(start).Milliseconds(),
	}, nil
}
