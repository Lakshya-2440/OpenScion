package local

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/tree"
	"github.com/google/uuid"
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

	currentTree, err := tree.BuildTreeFromDir(r.WorkDir, r.Store, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	diffs := tree.Diff(headTree, currentTree)
	return diffs, currentTree, headCS, nil
}

// Commit records modified files as a new Changeset in the local DAG
func (r *LocalRepo) Commit(message, author, agentModel, sessionID, taskTraceID string) (*graph.Changeset, error) {
	headCS, err := r.GetHeadChangeset()
	if err != nil {
		return nil, err
	}

	currentTree, err := tree.BuildTreeFromDir(r.WorkDir, r.Store, nil)
	if err != nil {
		return nil, err
	}

	headTree, _ := r.DB.GetTree(headCS.RootTreeHash)
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

// Log returns commit history
func (r *LocalRepo) Log(limit int) ([]graph.Changeset, error) {
	return r.DB.GetChangesetsByRepo(r.Config.RepoID, limit)
}

// Blame computes line-by-line blame for a file
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

	lines := strings.Split(string(content), "\n")
	var result []string
	for idx, line := range lines {
		result = append(result, fmt.Sprintf("%4d | #%-3d %-8s %-12s %s",
			idx+1, headCS.SequenceNumber, headCS.ShortID(), headCS.Author, line))
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
