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

	// 2. Fetch local changesets
	localChangesets, err := r.DB.GetChangesetsByRepo(r.Config.RepoID, 100)
	if err != nil {
		return nil, err
	}

	// Commits that need pushing (SequenceNumber > remote or not yet landed)
	var toPush []graph.Changeset
	for _, cs := range localChangesets {
		if cs.SequenceNumber > repoInfo.SequenceNumber || (repoInfo.SequenceNumber <= 1 && cs.SequenceNumber >= 1) {
			toPush = append(toPush, cs)
		}
	}

	if len(toPush) == 0 && repoInfo.SequenceNumber >= headCS.SequenceNumber {
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

	for _, cs := range toPush {
		// Load tree
		t, err := r.DB.GetTree(cs.RootTreeHash)
		if err != nil {
			continue
		}

		// Find and upload all CAS chunks for files in tree
		var chunksToUpload []*cas.BlobChunk
		for _, entry := range t.Entries {
			manifest, err := r.Store.GetManifest(entry.Hash)
			if err != nil {
				continue
			}
			for _, cHash := range manifest.ChunkHashes {
				chunk, err := r.Store.GetChunk(cHash)
				if err == nil {
					chunksToUpload = append(chunksToUpload, chunk)
					res.UploadedBytes += int64(len(chunk.Data))
				}
			}
		}

		if len(chunksToUpload) > 0 {
			if err := c.UploadChunks(chunksToUpload); err != nil {
				return nil, fmt.Errorf("failed to upload CAS chunks: %w", err)
			}
			res.UploadedChunks += len(chunksToUpload)
		}

		// Save Merkle tree on server
		if err := c.SaveTree(t); err != nil {
			return nil, fmt.Errorf("failed to save Merkle tree on server: %w", err)
		}

		// Submit to speculative rebase linear queue
		submitRes, err := c.SubmitChangeset(repoInfo.Repo.ID, &cs)
		if err != nil {
			return nil, fmt.Errorf("failed to submit changeset to merge queue: %w", err)
		}

		res.PushedChangesets++
		res.RemoteSequence = submitRes.SequenceNumber
		res.LandedCommit = submitRes.LandedCSID
		res.Message = cs.Message
	}

	// Update local branch tip sequence number to match global remote sequence
	if res.RemoteSequence > headCS.SequenceNumber {
		_ = r.DB.UpdateBranchTip(r.Config.RepoID, r.Config.Branch, headCS.ID, res.RemoteSequence)
	}

	return res, nil
}

// Pull downloads missing changesets and CAS chunks from remote and updates working copy
func (r *LocalRepo) Pull() (*PullResult, error) {
	if r.Config.RemoteURL == "" {
		return nil, fmt.Errorf("no remote server configured.\nRun: scion remote add origin <http://server-address:port>")
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

	if repoInfo.SequenceNumber <= headCS.SequenceNumber {
		return &PullResult{
			NewSequence: headCS.SequenceNumber,
		}, nil
	}

	diffCount := int(repoInfo.SequenceNumber - headCS.SequenceNumber)
	remoteLog, err := c.QueryLog(r.Config.RepoName, diffCount+10)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote log: %w", err)
	}

	// Filter missing changesets
	var missing []graph.Changeset
	for _, cs := range remoteLog {
		if cs.SequenceNumber > headCS.SequenceNumber {
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
		latestTree = remoteTree

		// Collect missing chunk hashes
		var missingHashes []string
		for _, entry := range remoteTree.Entries {
			if _, err := r.Store.GetBlobByHash(entry.Hash); err != nil {
				missingHashes = append(missingHashes, entry.Hash)
			}
		}

		if len(missingHashes) > 0 {
			chunks, err := c.DownloadChunks(missingHashes)
			if err == nil {
				for _, chunk := range chunks {
					r.Store.PutChunk(chunk)
					res.DownloadedChunks++
					res.DownloadedBytes += int64(len(chunk.Data))
				}
			}
		}

		r.DB.SaveTree(remoteTree)
		r.DB.SaveChangeset(&cs)
		res.PulledChangesets++
	}

	if latestTree != nil && len(missing) > 0 {
		newestCS := missing[len(missing)-1]
		_ = r.DB.UpdateBranchTip(r.Config.RepoID, r.Config.Branch, newestCS.ID, newestCS.SequenceNumber)
		res.UpdatedFiles = r.MaterializeTree(latestTree)
	}

	return res, nil
}

// MaterializeTree extracts files from the Merkle tree to the local working directory
func (r *LocalRepo) MaterializeTree(t *tree.Tree) int {
	count := 0
	for relPath, entry := range t.Entries {
		data, err := r.Store.GetBlobByHash(entry.Hash)
		if err != nil {
			continue
		}
		fullPath := filepath.Join(r.WorkDir, filepath.FromSlash(relPath))
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		if err := os.WriteFile(fullPath, data, os.FileMode(entry.Mode)); err == nil {
			count++
		}
	}
	return count
}
