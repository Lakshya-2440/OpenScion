package local

import (
	"fmt"
	"time"

	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/tree"
	"github.com/google/uuid"
)

// SaveStash saves uncommitted working directory changes into stash and reverts to HEAD clean state
func (r *LocalRepo) SaveStash(message string) (*db.Stash, error) {
	unlock, err := r.AcquireLock("stash")
	if err != nil {
		return nil, err
	}
	defer unlock()

	diffs, currentTree, headCS, err := r.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to check status: %w", err)
	}

	if len(diffs) == 0 {
		return nil, fmt.Errorf("no local changes to save")
	}

	// Ensure stashed tree is saved in DB
	if err := r.DB.SaveTree(currentTree); err != nil {
		return nil, fmt.Errorf("failed to save stashed tree: %w", err)
	}

	if message == "" {
		message = fmt.Sprintf("WIP on %s: %s %s", r.Config.Branch, headCS.ShortID(), headCS.Message)
	}

	stash := &db.Stash{
		ID:              uuid.New().String(),
		RepoID:          r.Config.RepoID,
		Branch:          r.Config.Branch,
		BaseChangesetID: headCS.ID,
		TreeHash:        currentTree.RootHash,
		Message:         message,
		CreatedAt:       time.Now().Unix(),
	}

	if err := r.DB.SaveStash(stash); err != nil {
		return nil, fmt.Errorf("failed to record stash: %w", err)
	}

	// Fast delta restore of working directory to clean HEAD state (avoids re-writing monorepo)
	headTree, err := r.DB.GetTree(headCS.RootTreeHash)
	if err != nil {
		headTree = tree.NewTree()
	}
	r.MaterializeTreeDelta(currentTree, headTree)

	return stash, nil
}

// ListStashes returns all saved stashes ordered newest to oldest
func (r *LocalRepo) ListStashes() ([]*db.Stash, error) {
	return r.DB.ListStashes(r.Config.RepoID)
}

// ApplyStash applies stashed changes at given index (0-based) to current working tree without dropping
func (r *LocalRepo) ApplyStash(index int) (*db.Stash, error) {
	stashes, err := r.ListStashes()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(stashes) {
		return nil, fmt.Errorf("stash@{n} index %d out of bounds (total: %d)", index, len(stashes))
	}
	stash := stashes[index]

	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, fmt.Errorf("cannot get HEAD changeset: %w", err)
	}

	headTree, err := r.DB.GetTree(headCS.RootTreeHash)
	if err != nil {
		headTree = tree.NewTree()
	}

	stashedTree, err := r.DB.GetTree(stash.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("stashed tree %s not found: %w", stash.TreeHash, err)
	}

	// Apply stashed delta to working directory
	r.MaterializeTreeDelta(headTree, stashedTree)
	return stash, nil
}

// PopStash applies stashed changes at given index and deletes stash record
func (r *LocalRepo) PopStash(index int) (*db.Stash, error) {
	unlock, err := r.AcquireLock("stash")
	if err != nil {
		return nil, err
	}
	defer unlock()

	stash, err := r.ApplyStash(index)
	if err != nil {
		return nil, err
	}

	if err := r.DB.DeleteStash(stash.ID); err != nil {
		return nil, fmt.Errorf("applied stash but failed to drop record: %w", err)
	}
	return stash, nil
}

// DropStash deletes stash record at given index
func (r *LocalRepo) DropStash(index int) (*db.Stash, error) {
	unlock, err := r.AcquireLock("stash")
	if err != nil {
		return nil, err
	}
	defer unlock()

	stashes, err := r.ListStashes()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(stashes) {
		return nil, fmt.Errorf("stash@{n} index %d out of bounds (total: %d)", index, len(stashes))
	}
	stash := stashes[index]

	if err := r.DB.DeleteStash(stash.ID); err != nil {
		return nil, err
	}
	return stash, nil
}

// ShowStash returns file diffs for a given stash index compared to its base commit
func (r *LocalRepo) ShowStash(index int) ([]tree.DiffEntry, error) {
	stashes, err := r.ListStashes()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(stashes) {
		return nil, fmt.Errorf("stash@{n} index %d out of bounds (total: %d)", index, len(stashes))
	}
	stash := stashes[index]

	stashedTree, err := r.DB.GetTree(stash.TreeHash)
	if err != nil {
		return nil, err
	}

	baseCS, err := r.DB.GetChangeset(stash.BaseChangesetID)
	if err != nil {
		return nil, err
	}

	baseTree, err := r.DB.GetTree(baseCS.RootTreeHash)
	if err != nil {
		baseTree = tree.NewTree()
	}

	return tree.Diff(baseTree, stashedTree), nil
}
