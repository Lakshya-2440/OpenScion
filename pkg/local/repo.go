package local

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/merge"
	"github.com/aether/scn/pkg/tree"
	"github.com/google/uuid"
	"syscall"
)

type Config struct {
	RepoName       string   `json:"repo_name"`
	RepoID         string   `json:"repo_id"`
	Branch         string   `json:"branch"`
	AgentModel     string   `json:"agent_model"`
	SessionID      string   `json:"session_id"`
	RemoteURL      string   `json:"remote_url,omitempty"`
	SparsePatterns []string `json:"sparse_patterns,omitempty"`
}

type LocalRepo struct {
	WorkDir  string
	ScionDir string
	Config   Config
	Store    *cas.Store
	DB       *db.DB
}

// FindRepoRoot traverses upwards to find the directory containing .scion
func FindRepoRoot(startDir string) (string, error) {
	curr := startDir
	for {
		if fi, err := os.Stat(filepath.Join(curr, ".scion")); err == nil && fi.IsDir() {
			return curr, nil
		}
		// Also support .scn for compatibility
		if fi, err := os.Stat(filepath.Join(curr, ".scn")); err == nil && fi.IsDir() {
			return curr, nil
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return "", fmt.Errorf("not an OpenScion repository (or any parent directory): .scion")
}

// InitLocalRepo creates a new self-contained OpenScion repository locally
func InitLocalRepo(workDir, repoName string) (*LocalRepo, error) {
	scionDir := filepath.Join(workDir, ".scion")
	if _, err := os.Stat(scionDir); err == nil {
		return nil, fmt.Errorf("repository already exists at %s", scionDir)
	}

	casDir := filepath.Join(scionDir, "cas")
	if err := os.MkdirAll(casDir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(scionDir, "metadata.db")
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize repository database: %w", err)
	}

	store, err := cas.NewStore(casDir)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to initialize CAS storage: %w", err)
	}

	// Create repository record in DB
	repoRec, err := database.CreateRepo(repoName)
	if err != nil {
		database.Close()
		return nil, err
	}

	cfg := Config{
		RepoName:       repoName,
		RepoID:         repoRec.ID,
		Branch:         "main",
		AgentModel:     detectAgentModel(),
		SessionID:      uuid.New().String(),
		SparsePatterns: []string{"*"},
	}

	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(scionDir, "config.json"), cfgData, 0644); err != nil {
		database.Close()
		return nil, err
	}

	// Auto-generate agent instructions so AI coding agents use OpenScion automatically
	createAgentInstructions(workDir, repoName)

	return &LocalRepo{
		WorkDir:  workDir,
		ScionDir: scionDir,
		Config:   cfg,
		Store:    store,
		DB:       database,
	}, nil
}

// OpenLocalRepo opens an existing local OpenScion repository
func OpenLocalRepo(workDir string) (*LocalRepo, error) {
	root, err := FindRepoRoot(workDir)
	if err != nil {
		return nil, err
	}

	scionDir := filepath.Join(root, ".scion")
	if _, err := os.Stat(scionDir); err != nil {
		scionDir = filepath.Join(root, ".scn")
	}

	cfgData, err := os.ReadFile(filepath.Join(scionDir, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("missing config.json in %s", scionDir)
	}

	var cfg Config
	if err := json.Unmarshal(cfgData, &cfg); err != nil {
		return nil, fmt.Errorf("corrupt config.json: %w", err)
	}

	dbPath := filepath.Join(scionDir, "metadata.db")
	// If metadata.db doesn't exist (e.g. from old client clone), create it
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository database: %w", err)
	}

	casDir := filepath.Join(scionDir, "cas")
	if _, err := os.Stat(casDir); err != nil {
		casDir = filepath.Join(scionDir, "cache")
	}
	store, err := cas.NewStore(casDir)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to open CAS: %w", err)
	}

	return &LocalRepo{
		WorkDir:  root,
		ScionDir: scionDir,
		Config:   cfg,
		Store:    store,
		DB:       database,
	}, nil
}

func (r *LocalRepo) Close() {
	if r.DB != nil {
		r.DB.Close()
	}
}

// GetHeadChangeset returns current HEAD commit
func (r *LocalRepo) GetHeadChangeset() (*graph.Changeset, error) {
	branch, err := r.DB.GetBranch(r.Config.RepoID, r.Config.Branch)
	if err != nil {
		return nil, err
	}
	return r.DB.GetChangeset(branch.HeadID)
}

// ResolveChangeset resolves a reference (HEAD, sequence #3 or 3, or commit ID) to a Changeset
func (r *LocalRepo) ResolveChangeset(ref string) (*graph.Changeset, error) {
	if ref == "" || ref == "HEAD" {
		return r.GetHeadChangeset()
	}

	clean := strings.TrimPrefix(ref, "#")
	if seq, err := strconv.ParseInt(clean, 10, 64); err == nil {
		return r.DB.GetChangesetBySequence(r.Config.RepoID, seq)
	}

	// Try direct ID lookup
	cs, err := r.DB.GetChangeset(ref)
	if err == nil {
		return cs, nil
	}

	// Prefix search in recent changesets
	changesets, _ := r.DB.GetChangesetsByRepo(r.Config.RepoID, 200)
	for _, c := range changesets {
		if strings.HasPrefix(c.ID, ref) {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("changeset %q not found", ref)
}


// AcquireLock acquires an exclusive lock on the workspace to prevent concurrent writes
func (r *LocalRepo) AcquireLock(op string) (func(), error) {
	lockPath := filepath.Join(r.ScionDir, "lock")
	payload := fmt.Sprintf("%d:%s", os.Getpid(), op)

	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			_, writeErr := f.WriteString(payload)
			_ = f.Close()
			if writeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("failed to write workspace lock payload: %w", writeErr)
			}
			unlock := func() {
				_ = os.Remove(lockPath)
			}
			return unlock, nil
		}

		if os.IsExist(err) {
			if data, readErr := os.ReadFile(lockPath); readErr == nil {
				parts := strings.Split(string(data), ":")
				if len(parts) >= 2 {
					pid, _ := strconv.Atoi(parts[0])
					if pid > 0 && isProcessAlive(pid) {
						return nil, fmt.Errorf("workspace locked by process PID %d (%s)", pid, parts[1])
					}
				}
				// Dead process left stale lock
				_ = os.Remove(lockPath)
				continue
			}
		}

		return nil, fmt.Errorf("failed to acquire workspace lock: %w", err)
	}

	return nil, fmt.Errorf("workspace locked by another process")
}

func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// Status inspects the working copy against HEAD
func (r *LocalRepo) Status() ([]tree.DiffEntry, *tree.Tree, *graph.Changeset, error) {
	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, nil, nil, err
	}

	headTree, err := r.DB.GetTree(headCS.RootTreeHash)
	if err != nil {
		headTree = tree.NewTree()
	}

	currentTree, err := tree.BuildTreeFromDirWithBaseAndSparse(r.WorkDir, r.Store, nil, headTree, r.Config.SparsePatterns)
	if err != nil {
		return nil, nil, nil, err
	}

	diffs := tree.Diff(headTree, currentTree)
	return diffs, currentTree, headCS, nil
}

// Commit records modified files as a new Changeset in the local DAG
func (r *LocalRepo) Commit(message, author, agentModel, sessionID, taskTraceID string) (*graph.Changeset, error) {
	unlock, err := r.AcquireLock("commit")
	if err != nil {
		return nil, err
	}
	defer unlock()

	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, err
	}

	headTree, _ := r.DB.GetTree(headCS.RootTreeHash)
	currentTree, err := tree.BuildTreeFromDirWithBaseAndSparse(r.WorkDir, r.Store, nil, headTree, r.Config.SparsePatterns)
	if err != nil {
		return nil, err
	}

	diffs := tree.Diff(headTree, currentTree)
	if len(diffs) == 0 {
		return nil, fmt.Errorf("nothing to commit, working tree clean")
	}

	// Save tree
	if err := r.DB.SaveTree(currentTree); err != nil {
		return nil, err
	}

	if author == "" {
		author = getGitAuthor()
	}
	if agentModel == "" {
		agentModel = r.Config.AgentModel
	}
	if sessionID == "" {
		sessionID = r.Config.SessionID
	}

	nextSeq := headCS.SequenceNumber + 1
	cs := graph.NewChangeset(
		r.Config.RepoID,
		currentTree.RootHash,
		[]string{headCS.ID},
		author,
		message,
		agentModel,
		sessionID,
		taskTraceID,
	)
	cs.SequenceNumber = nextSeq

	if err := r.DB.SaveChangeset(cs); err != nil {
		return nil, err
	}

	if err := r.DB.UpdateBranchTip(r.Config.RepoID, r.Config.Branch, cs.ID, nextSeq); err != nil {
		return nil, err
	}

	return cs, nil
}

// Log returns commit history for the current branch by walking the ancestry DAG
func (r *LocalRepo) Log(limit int) ([]graph.Changeset, error) {
	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}

	var history []graph.Changeset
	curr := headCS
	visited := make(map[string]bool)

	for curr != nil && len(history) < limit {
		if visited[curr.ID] {
			break
		}
		visited[curr.ID] = true
		history = append(history, *curr)

		if len(curr.ParentIDs) == 0 {
			break
		}
		parentCS, err := r.DB.GetChangeset(curr.ParentIDs[0])
		if err != nil {
			break
		}
		curr = parentCS
	}

	return history, nil
}

// GC walks all branch heads, collects all reachable trees, manifests, and chunks,
// and deletes unreferenced objects from local CAS storage
func (r *LocalRepo) GC() (int, int64, error) {
	unlock, err := r.AcquireLock("gc")
	if err != nil {
		return 0, 0, err
	}
	defer unlock()

	branches, err := r.ListBranches()
	if err != nil {
		return 0, 0, err
	}

	reachable := make(map[string]bool)
	visitedCS := make(map[string]bool)

	var queue []string
	for _, b := range branches {
		if b.HeadID != "" {
			queue = append(queue, b.HeadID)
		}
	}

	for len(queue) > 0 {
		csID := queue[0]
		queue = queue[1:]

		if visitedCS[csID] {
			continue
		}
		visitedCS[csID] = true

		cs, err := r.DB.GetChangeset(csID)
		if err != nil {
			continue
		}

		for _, pID := range cs.ParentIDs {
			if !visitedCS[pID] {
				queue = append(queue, pID)
			}
		}

		t, err := r.DB.GetTree(cs.RootTreeHash)
		if err != nil {
			continue
		}

		for _, entry := range t.Entries {
			reachable[entry.Hash] = true
			manifest, err := r.Store.GetManifest(entry.Hash)
			if err == nil {
				reachable[manifest.BlobHash] = true
				for _, ch := range manifest.ChunkHashes {
					reachable[ch] = true
				}
			}
		}
	}

	return r.Store.GC(reachable)
}

// Blame computes line-by-line blame for a file by walking backwards through DAG history
func (r *LocalRepo) Blame(relPath string) ([]string, error) {
	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, err
	}

	headTree, err := r.DB.GetTree(headCS.RootTreeHash)
	if err != nil {
		return nil, err
	}

	entry, ok := headTree.Entries[relPath]
	if !ok {
		return nil, fmt.Errorf("file %q not found in repository", relPath)
	}

	content, err := r.Store.GetBlobByHash(entry.Hash)
	if err != nil {
		return nil, err
	}

	headLines := strings.Split(string(content), "\n")
	if len(headLines) == 0 {
		return nil, nil
	}

	attributions := make([]*graph.Changeset, len(headLines))
	currentLineMap := make([]int, len(headLines))
	for i := range currentLineMap {
		currentLineMap[i] = i
	}

	currCS := headCS
	currLines := headLines

	for currCS != nil {
		if len(currCS.ParentIDs) == 0 {
			for _, origIdx := range currentLineMap {
				if attributions[origIdx] == nil {
					attributions[origIdx] = currCS
				}
			}
			break
		}

		parentCS, err := r.DB.GetChangeset(currCS.ParentIDs[0])
		if err != nil || parentCS == nil {
			for _, origIdx := range currentLineMap {
				if attributions[origIdx] == nil {
					attributions[origIdx] = currCS
				}
			}
			break
		}

		parentTree, err := r.DB.GetTree(parentCS.RootTreeHash)
		if err != nil {
			for _, origIdx := range currentLineMap {
				if attributions[origIdx] == nil {
					attributions[origIdx] = currCS
				}
			}
			break
		}

		parentEntry, parentHasFile := parentTree.Entries[relPath]
		if !parentHasFile {
			for _, origIdx := range currentLineMap {
				if attributions[origIdx] == nil {
					attributions[origIdx] = currCS
				}
			}
			break
		}

		parentContent, err := r.Store.GetBlobByHash(parentEntry.Hash)
		if err != nil {
			for _, origIdx := range currentLineMap {
				if attributions[origIdx] == nil {
					attributions[origIdx] = currCS
				}
			}
			break
		}

		parentLines := strings.Split(string(parentContent), "\n")
		edits := merge.MyersDiff(parentLines, currLines)

		nextParentLineMap := make([]int, len(parentLines))
		for k := range nextParentLineMap {
			nextParentLineMap[k] = -1
		}

		currIdx := 0
		parentIdx := 0
		for _, e := range edits {
			switch e.Type {
			case merge.EditEqual:
				if currIdx < len(currentLineMap) && parentIdx < len(nextParentLineMap) {
					origIdx := currentLineMap[currIdx]
					nextParentLineMap[parentIdx] = origIdx
				}
				currIdx++
				parentIdx++
			case merge.EditDelete:
				parentIdx++
			case merge.EditInsert:
				if currIdx < len(currentLineMap) {
					origIdx := currentLineMap[currIdx]
					if attributions[origIdx] == nil {
						attributions[origIdx] = currCS
					}
				}
				currIdx++
			}
		}

		var filteredParentMap []int
		var filteredParentLines []string
		for pIdx, origIdx := range nextParentLineMap {
			if origIdx != -1 {
				filteredParentMap = append(filteredParentMap, origIdx)
				filteredParentLines = append(filteredParentLines, parentLines[pIdx])
			}
		}

		if len(filteredParentMap) == 0 {
			break
		}

		currentLineMap = filteredParentMap
		currLines = filteredParentLines
		currCS = parentCS
	}

	for i := range attributions {
		if attributions[i] == nil {
			attributions[i] = headCS
		}
	}

	var result []string
	for idx, line := range headLines {
		cs := attributions[idx]
		result = append(result, fmt.Sprintf("%4d | #%-3d %-8s %-16s %s",
			idx+1, cs.SequenceNumber, cs.ShortID(), cs.Author, line))
	}
	return result, nil
}

func detectAgentModel() string {
	if m := os.Getenv("AGENT_MODEL"); m != "" {
		return m
	}
	if os.Getenv("CLAUDE_CODE") != "" || os.Getenv("CLAUDE_SHELL") != "" {
		return "claude-code-3.7-sonnet"
	}
	return "human"
}

func getGitAuthor() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "developer"
	}
	return fmt.Sprintf("%s <%s@local>", user, user)
}

func createAgentInstructions(workDir, repoName string) {
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		content := `# Agent Instruction Protocol — OpenScion VCS

This repository uses **OpenScion** (` + "`scn`" + ` / ` + "`scion`" + `) for version control, **NOT Git**.
Do NOT run ` + "`git add`" + ` or ` + "`git commit`" + `.

## Required Agent Commands
- Check working tree: ` + "`scn status`" + `
- Inspect diffs: ` + "`scn diff`" + `
- Record changesets: ` + "`scn commit -m \"<message>\" --agent <your-model-name>`" + `
- View history: ` + "`scn log`" + `
- Inspect commits: ` + "`scn show <#seq|hash>`" + `
- Sync with remote: ` + "`scn pull`" + ` / ` + "`scn push`" + `

Every commit must include ` + "`--agent <model>`" + ` for provenance tracking.
`
		_ = os.WriteFile(agentsPath, []byte(content), 0644)
	}

	claudePath := filepath.Join(workDir, "CLAUDE.md")
	if _, err := os.Stat(claudePath); os.IsNotExist(err) {
		content := `# Claude Code Instructions — OpenScion VCS

This repository uses **OpenScion** (` + "`scn`" + `) instead of Git.
- Always use ` + "`scn status`" + `, ` + "`scn diff`" + `, and ` + "`scn commit -m \"...\" --agent claude-code`" + `.
- Never use Git commands in this workspace.
`
		_ = os.WriteFile(claudePath, []byte(content), 0644)
	}
}

// ListBranches returns all branches in the local repository
func (r *LocalRepo) ListBranches() ([]db.Branch, error) {
	return r.DB.ListBranches(r.Config.RepoID)
}

// CreateBranch creates a new branch pointing to current HEAD
func (r *LocalRepo) CreateBranch(branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return fmt.Errorf("branch name cannot be empty")
	}

	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return err
	}

	return r.DB.CreateBranch(r.Config.RepoID, branchName, headCS.ID, headCS.SequenceNumber)
}

// DeleteBranch removes a branch from local repository
func (r *LocalRepo) DeleteBranch(branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	if branchName == r.Config.Branch {
		return fmt.Errorf("cannot delete currently checked out branch %q", branchName)
	}
	if branchName == "main" {
		return fmt.Errorf("cannot delete default branch 'main'")
	}
	return r.DB.DeleteBranch(r.Config.RepoID, branchName)
}

// SwitchBranch switches current working branch and materializes its tree
func (r *LocalRepo) SwitchBranch(branchName string) error {
	unlock, err := r.AcquireLock("switch-branch")
	if err != nil {
		return err
	}
	defer unlock()

	targetBranch, err := r.DB.GetBranch(r.Config.RepoID, branchName)
	if err != nil {
		return fmt.Errorf("branch %q does not exist: %w", branchName, err)
	}

	diffs, _, _, err := r.Status()
	if err == nil && len(diffs) > 0 {
		return fmt.Errorf("cannot switch branches with uncommitted changes. Commit or discard them first.")
	}

	currCS, _ := r.GetHeadChangeset()
	var oldTree *tree.Tree
	if currCS != nil {
		oldTree, _ = r.DB.GetTree(currCS.RootTreeHash)
	}

	targetCS, err := r.DB.GetChangeset(targetBranch.HeadID)
	if err != nil {
		return fmt.Errorf("failed to load target branch commit: %w", err)
	}

	targetTree, err := r.DB.GetTree(targetCS.RootTreeHash)
	if err != nil {
		return fmt.Errorf("failed to load target branch tree: %w", err)
	}

	r.Config.Branch = branchName
	if err := r.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save repository config: %w", err)
	}

	r.MaterializeTreeDelta(oldTree, targetTree)
	return nil
}

// RestoreFile restores a specific file from HEAD or a target revision
func (r *LocalRepo) RestoreFile(relPath string, rev string) error {
	unlock, err := r.AcquireLock("restore-file")
	if err != nil {
		return err
	}
	defer unlock()

	var cs *graph.Changeset
	if rev != "" {
		resolved, err := r.ResolveChangeset(rev)
		if err != nil {
			return err
		}
		cs = resolved
	} else {
		head, err := r.GetHeadChangeset()
		if err != nil {
			return err
		}
		cs = head
	}

	t, err := r.DB.GetTree(cs.RootTreeHash)
	if err != nil {
		return fmt.Errorf("failed to load tree for commit %s: %w", cs.ShortID(), err)
	}

	normalized := filepath.ToSlash(relPath)
	entry, ok := t.Entries[normalized]
	if !ok {
		return fmt.Errorf("file %q not found in commit %s", relPath, cs.ShortID())
	}

	data, err := r.Store.GetBlobByHash(entry.Hash)
	if err != nil {
		return fmt.Errorf("failed to retrieve blob for %s: %w", relPath, err)
	}

	fullPath := filepath.Join(r.WorkDir, filepath.FromSlash(relPath))
	_ = os.MkdirAll(filepath.Dir(fullPath), 0755)
	return os.WriteFile(fullPath, data, os.FileMode(entry.Mode))
}

// RestoreAll restores all files in working directory from HEAD or a target revision
func (r *LocalRepo) RestoreAll(rev string) (int, error) {
	unlock, err := r.AcquireLock("restore-all")
	if err != nil {
		return 0, err
	}
	defer unlock()

	var cs *graph.Changeset
	if rev != "" {
		resolved, err := r.ResolveChangeset(rev)
		if err != nil {
			return 0, err
		}
		cs = resolved
	} else {
		head, err := r.GetHeadChangeset()
		if err != nil {
			return 0, err
		}
		cs = head
	}

	t, err := r.DB.GetTree(cs.RootTreeHash)
	if err != nil {
		return 0, fmt.Errorf("failed to load tree for commit %s: %w", cs.ShortID(), err)
	}

	count := r.MaterializeTreeDelta(nil, t)
	return count, nil
}

