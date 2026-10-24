package local

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/client"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/merge"
	"github.com/aether/scn/pkg/queue"
	"github.com/aether/scn/pkg/tree"
)

type SyncResult struct {
	PushedChangesets int
	UploadedChunks   int
	UploadedBytes    int64
	RemoteSequence   int64
	LandedCommit     string
	Message          string
}

type PullResult struct {
	PulledChangesets int
	DownloadedChunks int
	DownloadedBytes  int64
	UpdatedFiles     int
	NewSequence      int64
}

// SetRemote updates the repository remote URL
func (r *LocalRepo) SetRemote(url string) error {
	r.Config.RemoteURL = strings.TrimSpace(url)
	return r.SaveConfig()
}

// GetRemote returns current remote URL
func (r *LocalRepo) GetRemote() string {
	return r.Config.RemoteURL
}

// SaveConfig persists the current configuration to .scion/config.json
func (r *LocalRepo) SaveConfig() error {
	data, err := json.MarshalIndent(r.Config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.ScionDir, "config.json"), data, 0644)
}

// Push uploads unrecorded local commits, Merkle trees, and CAS chunks to the remote server
func (r *LocalRepo) Push() (*SyncResult, error) {
	unlock, err := r.AcquireLock("push")
	if err != nil {
		return nil, err
	}
	defer unlock()

	if r.Config.RemoteURL == "" {
		return nil, fmt.Errorf("no remote server configured.\nRun: scion remote add origin <http://server-address:port>")
	}

	c := client.NewClient(r.Config.RemoteURL)

	// 1. Verify or create repo on server
	repoInfo, err := c.GetRepoInfo(r.Config.RepoName)
	if err != nil {
		_, createErr := c.CreateRepo(r.Config.RepoName)
		if createErr != nil {
			return nil, fmt.Errorf("cannot connect or create repo on remote %s: %w", r.Config.RemoteURL, err)
		}
		repoInfo, err = c.GetRepoInfo(r.Config.RepoName)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch remote repo info: %w", err)
		}
	}

	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, fmt.Errorf("failed to get local HEAD commit: %w", err)
	}

	// 2. Identify unpushed commits by walking back from local HEAD to find commits not yet on remote
	remoteKnown := make(map[string]bool)
	if repoInfo.Branch.HeadID != "" {
		remoteKnown[repoInfo.Branch.HeadID] = true
	}
	remoteLog, err := c.QueryLog(r.Config.RepoName, 100)
	if err != nil {
		return nil, fmt.Errorf("failed to query remote history: %w", err)
	}
	for _, rcs := range remoteLog {
		remoteKnown[rcs.ID] = true
	}

	var toPush []graph.Changeset
	visited := make(map[string]bool)
	walkQueue := []string{headCS.ID}

	for len(walkQueue) > 0 {
		currID := walkQueue[0]
		walkQueue = walkQueue[1:]

		if currID == "" || visited[currID] || remoteKnown[currID] {
			continue
		}
		visited[currID] = true

		currCS, err := r.DB.GetChangeset(currID)
		if err != nil || currCS == nil {
			continue
		}

		toPush = append(toPush, *currCS)
		for _, pid := range currCS.ParentIDs {
			if !remoteKnown[pid] && !visited[pid] {
				walkQueue = append(walkQueue, pid)
			}
		}
	}

	if len(toPush) == 0 {
		return &SyncResult{
			RemoteSequence: repoInfo.SequenceNumber,
			Message:        "Everything up-to-date",
		}, nil
	}

	// Push in chronological order (oldest to newest)
	for i, j := 0, len(toPush)-1; i < j; i, j = i+1, j-1 {
		toPush[i], toPush[j] = toPush[j], toPush[i]
	}

	res := &SyncResult{
		RemoteSequence: repoInfo.SequenceNumber,
	}

	var lastLandedID string
	for idx := range toPush {
		cs := &toPush[idx]
		// Re-parent chained commits if previous commit in batch was speculatively rebased
		if lastLandedID != "" && len(cs.ParentIDs) > 0 && cs.ParentIDs[0] != lastLandedID {
			cs.ParentIDs[0] = lastLandedID
			cs.ID = cs.ComputeID()
		}

		// Load tree
		t, err := r.DB.GetTree(cs.RootTreeHash)
		if err != nil {
			return nil, fmt.Errorf("local tree %s is missing: %w", cs.RootTreeHash, err)
		}
		if err := r.validateTreeObjects(t); err != nil {
			return nil, fmt.Errorf("cannot push changeset %s: %w", cs.ShortID(), err)
		}

		// Diff-only CAS chunk upload: compare against parent tree to avoid uploading unchanged files
		filesToUpload := make(map[string]tree.Entry)
		if len(cs.ParentIDs) > 0 {
			if parentCS, err := r.DB.GetChangeset(cs.ParentIDs[0]); err == nil && parentCS != nil {
				if parentTree, err := r.DB.GetTree(parentCS.RootTreeHash); err == nil && parentTree != nil {
					diffs := tree.Diff(parentTree, t)
					for _, d := range diffs {
						if d.ChangeType != tree.ChangeDeleted {
							if entry, ok := t.Entries[d.Path]; ok {
								filesToUpload[d.Path] = entry
							}
						}
					}
				}
			}
		}
		if len(filesToUpload) == 0 {
			// Fallback (root commit or parent tree unavailable): upload all tree entries
			filesToUpload = t.Entries
		}

		var chunksToUpload []*cas.BlobChunk
		var manifestsToUpload []*cas.BlobManifest
		for _, entry := range filesToUpload {
			manifest, err := r.Store.GetManifest(entry.Hash)
			if err != nil {
				return nil, fmt.Errorf("missing manifest %s for %s: %w", entry.Hash, entry.Path, err)
			}
			manifestsToUpload = append(manifestsToUpload, manifest)
			for _, cHash := range manifest.ChunkHashes {
				chunk, err := r.Store.GetChunk(cHash)
				if err != nil {
					return nil, fmt.Errorf("missing chunk %s for %s: %w", cHash, entry.Path, err)
				}
				if err := cas.ValidateChunk(chunk); err != nil {
					return nil, fmt.Errorf("invalid chunk %s for %s: %w", cHash, entry.Path, err)
				}
				chunksToUpload = append(chunksToUpload, chunk)
				res.UploadedBytes += int64(len(chunk.Data))
			}
		}

		if len(chunksToUpload) > 0 {
			if err := c.UploadChunks(chunksToUpload); err != nil {
				return nil, fmt.Errorf("failed to upload CAS chunks: %w", err)
			}
			res.UploadedChunks += len(chunksToUpload)
		}

		if len(manifestsToUpload) > 0 {
			if err := c.UploadManifests(manifestsToUpload); err != nil {
				return nil, fmt.Errorf("failed to upload CAS manifests: %w", err)
			}
		}

		// Save Merkle tree on server
		if err := c.SaveTree(t); err != nil {
			return nil, fmt.Errorf("failed to save Merkle tree on server: %w", err)
		}

		// Submit to speculative rebase linear queue
		submitRes, err := c.SubmitChangeset(repoInfo.Repo.ID, cs)
		if err != nil {
			return nil, fmt.Errorf("failed to submit changeset to merge queue: %w", err)
		}

		if submitRes.Status != queue.StatusMerged {
			if len(submitRes.Conflicts) > 0 {
				var paths []string
				for _, cf := range submitRes.Conflicts {
					paths = append(paths, cf.Path)
				}
				return nil, fmt.Errorf("push rejected: rebase conflict in %d file(s): %s\nRun 'scion pull' to fetch latest changes and resolve conflicts locally",
					len(paths), strings.Join(paths, ", "))
			}
			return nil, fmt.Errorf("push rejected: %s", submitRes.ErrorMessage)
		}

		res.PushedChangesets++
		res.RemoteSequence = submitRes.SequenceNumber
		res.LandedCommit = submitRes.LandedCSID
		res.Message = cs.Message
		lastLandedID = submitRes.LandedCSID
	}

	// Keep local ancestry consistent with the landed remote commit.
	if res.LandedCommit == headCS.ID && res.RemoteSequence > headCS.SequenceNumber {
		if err := r.DB.UpdateBranchTip(r.Config.RepoID, r.Config.Branch, res.LandedCommit, res.RemoteSequence); err != nil {
			return nil, fmt.Errorf("failed to update local branch after push: %w", err)
		}
	}

	// If server speculatively rebased our commit to a new landed commit, automatically pull and re-align
	if res.LandedCommit != "" && res.LandedCommit != headCS.ID {
		if _, err := r.pullUnlocked(); err != nil {
			return nil, fmt.Errorf("remote commit landed but local re-alignment failed: %w", err)
		}
	}

	return res, nil
}

// Pull downloads missing changesets and CAS chunks from remote and updates working copy
func (r *LocalRepo) Pull() (*PullResult, error) {
	unlock, err := r.AcquireLock("pull")
	if err != nil {
		return nil, err
	}
	defer unlock()
	return r.pullUnlocked()
}

func (r *LocalRepo) pullUnlocked() (*PullResult, error) {
	if r.Config.RemoteURL == "" {
		return nil, fmt.Errorf("no remote server configured.\nRun: scion remote add origin <http://server-address:port>")
	}

	// Safety check: ensure local working directory has no uncommitted changes
	diffs, _, _, err := r.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect local workspace before pull: %w", err)
	}
	if len(diffs) > 0 {
		return nil, fmt.Errorf("cannot pull with uncommitted local changes; please commit or stash your changes before pulling")
	}

	c := client.NewClient(r.Config.RemoteURL)
	repoInfo, err := c.GetRepoInfo(r.Config.RepoName)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to remote %s: %w", r.Config.RemoteURL, err)
	}

	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, err
	}

	if repoInfo.SequenceNumber <= headCS.SequenceNumber && repoInfo.Branch.HeadID == headCS.ID {
		return &PullResult{
			NewSequence: headCS.SequenceNumber,
		}, nil
	}

	diffCount := int(repoInfo.SequenceNumber - headCS.SequenceNumber)
	if diffCount < 20 {
		diffCount = 20
	}
	remoteLog, err := c.QueryLog(r.Config.RepoName, diffCount+50)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote log: %w", err)
	}

	// Filter missing changesets
	var missing []graph.Changeset
	for _, cs := range remoteLog {
		if cs.SequenceNumber > headCS.SequenceNumber || cs.ID == repoInfo.Branch.HeadID {
			missing = append(missing, cs)
		}
	}

	// Reverse to oldest first
	for i, j := 0, len(missing)-1; i < j; i, j = i+1, j-1 {
		missing[i], missing[j] = missing[j], missing[i]
	}

	res := &PullResult{
		NewSequence: repoInfo.SequenceNumber,
	}

	var latestTree *tree.Tree
	for _, cs := range missing {
		remoteTree, err := c.GetTree(cs.RootTreeHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get tree %s: %w", cs.RootTreeHash, err)
		}
		if err := remoteTree.Validate(); err != nil {
			return nil, fmt.Errorf("remote tree %s failed integrity validation: %w", cs.RootTreeHash, err)
		}
		latestTree = remoteTree

		// 1. Collect missing manifest hashes
		var missingManifestHashes []string
		for _, entry := range remoteTree.Entries {
			if _, err := r.Store.GetManifest(entry.Hash); err != nil {
				missingManifestHashes = append(missingManifestHashes, entry.Hash)
			}
		}

		if len(missingManifestHashes) > 0 {
			manifests, err := c.DownloadManifests(missingManifestHashes)
			if err != nil {
				return nil, fmt.Errorf("failed to download CAS manifests: %w", err)
			}
			for _, m := range manifests {
				if err := r.Store.SaveManifest(m); err != nil {
					return nil, fmt.Errorf("failed to save downloaded manifest %s: %w", m.BlobHash, err)
				}
			}
		}

		// 2. Collect missing chunk hashes
		var missingChunkHashes []string
		for _, entry := range remoteTree.Entries {
			manifest, err := r.Store.GetManifest(entry.Hash)
			if err != nil {
				return nil, fmt.Errorf("downloaded manifest %s is unavailable: %w", entry.Hash, err)
			}
			for _, cHash := range manifest.ChunkHashes {
				if _, err := r.Store.GetChunk(cHash); err != nil {
					missingChunkHashes = append(missingChunkHashes, cHash)
				}
			}
		}

		if len(missingChunkHashes) > 0 {
			chunks, err := c.DownloadChunks(missingChunkHashes)
			if err != nil {
				return nil, fmt.Errorf("failed to download CAS chunks: %w", err)
			}
			for _, chunk := range chunks {
				if err := r.Store.PutChunk(chunk); err != nil {
					return nil, fmt.Errorf("failed to save downloaded chunk %s: %w", chunk.Hash, err)
				}
				res.DownloadedChunks++
				res.DownloadedBytes += int64(len(chunk.Data))
			}
		}
		if err := r.validateTreeObjects(remoteTree); err != nil {
			return nil, fmt.Errorf("remote changeset %s has incomplete content: %w", cs.ShortID(), err)
		}

		if err := r.DB.SaveTree(remoteTree); err != nil {
			return nil, fmt.Errorf("failed to save remote tree: %w", err)
		}
		if err := r.DB.SaveChangeset(&cs); err != nil {
			return nil, fmt.Errorf("failed to save remote changeset: %w", err)
		}
		res.PulledChangesets++
	}

	if latestTree != nil && len(missing) > 0 {
		newestCS := missing[len(missing)-1]

		// Check if local headCS is an ancestor of newestCS (fast-forward path)
		isAncestor := false
		if headCS.ID == "" {
			isAncestor = true
		} else {
			visitedA := make(map[string]bool)
			queueA := []string{newestCS.ID}
			for len(queueA) > 0 {
				cid := queueA[0]
				queueA = queueA[1:]
				if cid == headCS.ID {
					isAncestor = true
					break
				}
				if visitedA[cid] {
					continue
				}
				visitedA[cid] = true
				if acs, err := r.DB.GetChangeset(cid); err == nil && acs != nil {
					queueA = append(queueA, acs.ParentIDs...)
				}
			}
		}

		if isAncestor {
			if err := r.DB.UpdateBranchTip(r.Config.RepoID, r.Config.Branch, newestCS.ID, newestCS.SequenceNumber); err != nil {
				return nil, fmt.Errorf("failed to update local branch: %w", err)
			}
			oldTree, _ := r.DB.GetTree(headCS.RootTreeHash)
			res.UpdatedFiles = r.MaterializeTreeDelta(oldTree, latestTree)
			if remaining, _, _, statusErr := r.Status(); statusErr != nil || len(remaining) > 0 {
				if statusErr != nil {
					return nil, fmt.Errorf("failed to verify materialized pull: %w", statusErr)
				}
				return nil, fmt.Errorf("pull completed but working tree is not synchronized (%d differences)", len(remaining))
			}
		} else {
			// Local commits diverged! Find common ancestor and 3-way speculative rebase
			localAncestors := make(map[string]bool)
			localRoots := make(map[string]*graph.Changeset)
			lQueue := []string{headCS.ID}
			for len(lQueue) > 0 {
				cid := lQueue[0]
				lQueue = lQueue[1:]
				if localAncestors[cid] {
					continue
				}
				localAncestors[cid] = true
				if acs, err := r.DB.GetChangeset(cid); err == nil && acs != nil {
					localRoots[acs.RootTreeHash] = acs
					lQueue = append(lQueue, acs.ParentIDs...)
				}
			}

			var baseCS *graph.Changeset
			rQueue := []string{newestCS.ID}
			visitedR := make(map[string]bool)
			for len(rQueue) > 0 {
				cid := rQueue[0]
				rQueue = rQueue[1:]
				acs, getErr := r.DB.GetChangeset(cid)
				if localAncestors[cid] {
					baseCS = acs
					break
				}
				if visitedR[cid] {
					continue
				}
				visitedR[cid] = true
				if getErr == nil && acs != nil {
					if matchingLocal, ok := localRoots[acs.RootTreeHash]; ok {
						baseCS = matchingLocal
						break
					}
					rQueue = append(rQueue, acs.ParentIDs...)
				}
			}
			if baseCS == nil && len(newestCS.ParentIDs) > 0 {
				baseCS, _ = r.DB.GetChangeset(newestCS.ParentIDs[0])
				if baseCS == nil {
					if remoteBase, queryErr := c.QueryChangeset(newestCS.ParentIDs[0]); queryErr == nil {
						if remoteBaseTree, treeErr := c.GetTree(remoteBase.RootTreeHash); treeErr == nil {
							if fetchErr := r.downloadTreeObjects(c, remoteBaseTree); fetchErr != nil {
								return nil, fmt.Errorf("failed to fetch merge base content: %w", fetchErr)
							}
							if saveErr := r.DB.SaveTree(remoteBaseTree); saveErr != nil {
								return nil, saveErr
							}
							if saveErr := r.DB.SaveChangeset(remoteBase); saveErr != nil {
								return nil, saveErr
							}
							baseCS = remoteBase
						}
					}
				}
			}

			var baseTree *tree.Tree
			if baseCS != nil {
				baseTree, _ = r.DB.GetTree(baseCS.RootTreeHash)
			}
			headTree, _ := r.DB.GetTree(headCS.RootTreeHash)

			mRes, err := merge.ThreeWayMerge(baseTree, headTree, latestTree, r.Store)
			if err != nil {
				return nil, fmt.Errorf("local pull merge error: %w", err)
			}

			if !mRes.Clean {
				var paths []string
				for _, cf := range mRes.Conflicts {
					paths = append(paths, cf.Path)
				}
				return nil, fmt.Errorf("pull conflict between unpushed local commits and remote in %d file(s): %s\nResolve conflicts or push your branch before pulling", len(paths), strings.Join(paths, ", "))
			}

			if err := r.DB.SaveTree(mRes.MergedTree); err != nil {
				return nil, err
			}

			rebasedCS := graph.NewChangeset(
				r.Config.RepoID,
				mRes.MergedTree.RootHash,
				[]string{newestCS.ID},
				headCS.Author,
				fmt.Sprintf("Merge remote tip into %s", headCS.Message),
				headCS.AgentModel,
				headCS.SessionID,
				headCS.TaskTraceID,
			)
			rebasedCS.SequenceNumber = newestCS.SequenceNumber + 1
			if err := r.DB.SaveChangeset(rebasedCS); err != nil {
				return nil, err
			}

			if err := r.DB.UpdateBranchTip(r.Config.RepoID, r.Config.Branch, rebasedCS.ID, rebasedCS.SequenceNumber); err != nil {
				return nil, err
			}
			res.UpdatedFiles = r.MaterializeTreeDelta(headTree, mRes.MergedTree)
			if remaining, _, _, statusErr := r.Status(); statusErr != nil || len(remaining) > 0 {
				if statusErr != nil {
					return nil, fmt.Errorf("failed to verify materialized merge: %w", statusErr)
				}
				return nil, fmt.Errorf("pull merge completed but working tree is not synchronized (%d differences)", len(remaining))
			}
			res.NewSequence = rebasedCS.SequenceNumber
		}
	}

	return res, nil
}

func (r *LocalRepo) validateTreeObjects(t *tree.Tree) error {
	if err := t.Validate(); err != nil {
		return err
	}
	for path, entry := range t.Entries {
		manifest, err := r.Store.GetManifest(entry.Hash)
		if err != nil {
			return fmt.Errorf("missing manifest for %s: %w", path, err)
		}
		if err := cas.ValidateManifest(manifest); err != nil {
			return err
		}
		data, err := r.Store.GetBlob(manifest)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}
		if int64(len(data)) != entry.Size || cas.HashBytes(data) != entry.Hash {
			return fmt.Errorf("content integrity check failed for %s", path)
		}
	}
	return nil
}

func (r *LocalRepo) downloadTreeObjects(c *client.Client, t *tree.Tree) error {
	if err := t.Validate(); err != nil {
		return err
	}
	manifestHashes := make([]string, 0)
	for _, entry := range t.Entries {
		if _, err := r.Store.GetManifest(entry.Hash); err != nil {
			manifestHashes = append(manifestHashes, entry.Hash)
		}
	}
	if len(manifestHashes) > 0 {
		manifests, err := c.DownloadManifests(manifestHashes)
		if err != nil {
			return err
		}
		for _, manifest := range manifests {
			if err := r.Store.SaveManifest(manifest); err != nil {
				return err
			}
		}
	}
	chunkHashes := make([]string, 0)
	for _, entry := range t.Entries {
		manifest, err := r.Store.GetManifest(entry.Hash)
		if err != nil {
			return err
		}
		for _, hash := range manifest.ChunkHashes {
			if _, err := r.Store.GetChunk(hash); err != nil {
				chunkHashes = append(chunkHashes, hash)
			}
		}
	}
	if len(chunkHashes) > 0 {
		chunks, err := c.DownloadChunks(chunkHashes)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			if err := r.Store.PutChunk(chunk); err != nil {
				return err
			}
		}
	}
	return r.validateTreeObjects(t)
}

// MaterializeTree extracts files from the Merkle tree to the local working directory
func (r *LocalRepo) MaterializeTree(t *tree.Tree) int {
	return r.MaterializeTreeDelta(nil, t)
}

// MaterializeTreeDelta updates working directory to match newTree, removing deleted files from oldTree
func (r *LocalRepo) MaterializeTreeDelta(oldTree, newTree *tree.Tree) int {
	if newTree == nil {
		return 0
	}
	count := 0

	// 1. Delete files present in oldTree but absent in newTree
	if oldTree != nil {
		for oldPath := range oldTree.Entries {
			if _, exists := newTree.Entries[oldPath]; !exists {
				fullPath := filepath.Join(r.WorkDir, filepath.FromSlash(oldPath))
				_ = os.Remove(fullPath)
				// Clean up empty directories upwards
				dir := filepath.Dir(fullPath)
				for dir != r.WorkDir && dir != "." && dir != "/" {
					if err := os.Remove(dir); err != nil {
						break // Not empty or permission issue
					}
					dir = filepath.Dir(dir)
				}
			}
		}
	}

	// 2. Materialize added or modified files
	for relPath, entry := range newTree.Entries {
		data, err := r.Store.GetBlobByHash(entry.Hash)
		if err != nil {
			continue
		}
		fullPath := filepath.Join(r.WorkDir, filepath.FromSlash(relPath))

		// If fullPath is currently a directory, remove it so file can be written
		if fi, err := os.Lstat(fullPath); err == nil && fi.IsDir() {
			_ = os.RemoveAll(fullPath)
		}
		// If any ancestor directory of fullPath is currently a regular file, remove that file
		for d := filepath.Dir(fullPath); d != r.WorkDir && d != "." && d != "/"; d = filepath.Dir(d) {
			if fi, err := os.Lstat(d); err == nil && !fi.IsDir() {
				_ = os.Remove(d)
			}
		}

		_ = os.MkdirAll(filepath.Dir(fullPath), 0755)

		if entry.Mode == 0120000 {
			_ = os.Remove(fullPath)
			if err := os.Symlink(string(data), fullPath); err == nil {
				count++
			}
		} else {
			_ = os.Remove(fullPath)
			if err := os.WriteFile(fullPath, data, os.FileMode(entry.Mode)); err == nil {
				count++
			}
		}
	}
	return count
}
