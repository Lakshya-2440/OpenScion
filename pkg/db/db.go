package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aether/scn/pkg/graph"
	"github.com/aether/scn/pkg/tree"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Repository struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

type Branch struct {
	RepoID         string `json:"repo_id"`
	Name           string `json:"name"`
	HeadID         string `json:"head_id"`
	SequenceNumber int64  `json:"sequence_number"`
	UpdatedAt      int64  `json:"updated_at"`
}

type SparseProfile struct {
	ID               string   `json:"id"`
	RepoID           string   `json:"repo_id"`
	Name             string   `json:"name"`
	IncludedPatterns []string `json:"included_patterns"`
	CreatedAt        int64    `json:"created_at"`
}

type DB struct {
	sqlDB *sql.DB
	mu    sync.Mutex
}

// Open initializes the database connection and runs schema migrations
func Open(dataSource string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", dataSource)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Optimize SQLite settings for high concurrency
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)

	d := &DB{sqlDB: sqlDB}
	if err := d.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	return d, nil
}

func (d *DB) Close() error {
	return d.sqlDB.Close()
}

func (d *DB) initSchema() error {
	queries := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA synchronous=NORMAL;`,
		`CREATE TABLE IF NOT EXISTS repositories (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS branches (
			repo_id TEXT NOT NULL,
			name TEXT NOT NULL,
			head_changeset_id TEXT NOT NULL,
			sequence_number INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (repo_id, name)
		);`,
		`CREATE TABLE IF NOT EXISTS changesets (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			sequence_number INTEGER NOT NULL DEFAULT 0,
			root_tree_hash TEXT NOT NULL,
			parent_ids TEXT NOT NULL,
			author TEXT NOT NULL,
			message TEXT NOT NULL,
			agent_model TEXT,
			session_id TEXT,
			task_trace_id TEXT,
			created_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_changesets_repo_seq ON changesets(repo_id, sequence_number DESC);`,
		`CREATE TABLE IF NOT EXISTS trees (
			hash TEXT PRIMARY KEY,
			data TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS sparse_profiles (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			name TEXT NOT NULL,
			included_patterns TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS merge_queue (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			changeset_id TEXT NOT NULL,
			status TEXT NOT NULL,
			speculative_parent TEXT,
			enqueued_at INTEGER NOT NULL,
			landed_at INTEGER
		);`,
	}

	for _, q := range queries {
		if _, err := d.sqlDB.Exec(q); err != nil {
			return fmt.Errorf("error executing %q: %w", q, err)
		}
	}
	return nil
}

// CreateRepo creates a new repository and initial empty main branch
func (d *DB) CreateRepo(name string) (*Repository, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	repoID := uuid.New().String()
	now := time.Now().Unix()

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT INTO repositories (id, name, created_at) VALUES (?, ?, ?)`,
		repoID, name, now)
	if err != nil {
		return nil, fmt.Errorf("repo %q already exists or error: %w", name, err)
	}

	// Initialize empty tree
	emptyTree := tree.NewTree()
	emptyTreeHash := emptyTree.ComputeHash()
	data, err := emptyTree.Serialize()
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`INSERT OR IGNORE INTO trees (hash, data) VALUES (?, ?)`, emptyTreeHash, string(data))
	if err != nil {
		return nil, err
	}

	// Initialize initial commit (Sequence #1)
	initialCS := graph.NewChangeset(
		repoID,
		emptyTreeHash,
		[]string{},
		"OpenScion System <system@openscion.local>",
		"Initial empty repository commit",
		"system",
		"",
		"",
	)
	initialCS.SequenceNumber = 1

	_, err = tx.Exec(`INSERT INTO changesets (
		id, repo_id, sequence_number, root_tree_hash, parent_ids,
		author, message, agent_model, session_id, task_trace_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		initialCS.ID, repoID, initialCS.SequenceNumber, initialCS.RootTreeHash, "",
		initialCS.Author, initialCS.Message, initialCS.AgentModel, initialCS.SessionID, initialCS.TaskTraceID, initialCS.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Set main branch
	_, err = tx.Exec(`INSERT INTO branches (repo_id, name, head_changeset_id, sequence_number, updated_at)
		VALUES (?, 'main', ?, 1, ?)`,
		repoID, initialCS.ID, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Repository{
		ID:        repoID,
		Name:      name,
		CreatedAt: now,
	}, nil
}

func (d *DB) GetRepo(name string) (*Repository, error) {
	row := d.sqlDB.QueryRow(`SELECT id, name, created_at FROM repositories WHERE name = ? OR id = ?`, name, name)
	var r Repository
	if err := row.Scan(&r.ID, &r.Name, &r.CreatedAt); err != nil {
		return nil, fmt.Errorf("repository %q not found: %w", name, err)
	}
	return &r, nil
}

func (d *DB) ListRepos() ([]Repository, error) {
	rows, err := d.sqlDB.Query(`SELECT id, name, created_at FROM repositories ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []Repository
	for rows.Next() {
		var r Repository
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedAt); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, nil
}

func (d *DB) GetBranch(repoID, branchName string) (*Branch, error) {
	row := d.sqlDB.QueryRow(`SELECT repo_id, name, head_changeset_id, sequence_number, updated_at
		FROM branches WHERE repo_id = ? AND name = ?`, repoID, branchName)
	var b Branch
	if err := row.Scan(&b.RepoID, &b.Name, &b.HeadID, &b.SequenceNumber, &b.UpdatedAt); err != nil {
		return nil, fmt.Errorf("branch %q not found for repo %s: %w", branchName, repoID, err)
	}
	return &b, nil
}

func (d *DB) UpdateBranchTip(repoID, branchName, newHeadID string, newSeq int64) error {
	now := time.Now().Unix()
	_, err := d.sqlDB.Exec(`UPDATE branches SET head_changeset_id = ?, sequence_number = ?, updated_at = ?
		WHERE repo_id = ? AND name = ?`, newHeadID, newSeq, now, repoID, branchName)
	return err
}

func (d *DB) SaveTree(t *tree.Tree) error {
	data, err := t.Serialize()
	if err != nil {
		return err
	}
	_, err = d.sqlDB.Exec(`INSERT OR REPLACE INTO trees (hash, data) VALUES (?, ?)`, t.RootHash, string(data))
	return err
}

func (d *DB) GetTree(hash string) (*tree.Tree, error) {
	row := d.sqlDB.QueryRow(`SELECT data FROM trees WHERE hash = ?`, hash)
	var raw string
	if err := row.Scan(&raw); err != nil {
		return nil, fmt.Errorf("tree hash %s not found: %w", hash, err)
	}
	return tree.Deserialize([]byte(raw))
}

func (d *DB) SaveChangeset(cs *graph.Changeset) error {
	parentIDsStr := strings.Join(cs.ParentIDs, ",")
	_, err := d.sqlDB.Exec(`INSERT OR REPLACE INTO changesets (
		id, repo_id, sequence_number, root_tree_hash, parent_ids,
		author, message, agent_model, session_id, task_trace_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cs.ID, cs.RepoID, cs.SequenceNumber, cs.RootTreeHash, parentIDsStr,
		cs.Author, cs.Message, cs.AgentModel, cs.SessionID, cs.TaskTraceID, cs.CreatedAt)
	return err
}

func (d *DB) GetChangeset(id string) (*graph.Changeset, error) {
	row := d.sqlDB.QueryRow(`SELECT id, repo_id, sequence_number, root_tree_hash, parent_ids,
		author, message, agent_model, session_id, task_trace_id, created_at
		FROM changesets WHERE id = ? OR id LIKE ?`, id, id+"%")

	var cs graph.Changeset
	var parentIDsStr string
	var agentModel, sessionID, taskTraceID sql.NullString

	if err := row.Scan(
		&cs.ID, &cs.RepoID, &cs.SequenceNumber, &cs.RootTreeHash, &parentIDsStr,
		&cs.Author, &cs.Message, &agentModel, &sessionID, &taskTraceID, &cs.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("changeset %s not found: %w", id, err)
	}

	if parentIDsStr != "" {
		cs.ParentIDs = strings.Split(parentIDsStr, ",")
	} else {
		cs.ParentIDs = []string{}
	}
	if agentModel.Valid {
		cs.AgentModel = agentModel.String
	}
	if sessionID.Valid {
		cs.SessionID = sessionID.String
	}
	if taskTraceID.Valid {
		cs.TaskTraceID = taskTraceID.String
	}

	return &cs, nil
}

func (d *DB) GetChangesetBySequence(repoID string, seq int64) (*graph.Changeset, error) {
	row := d.sqlDB.QueryRow(`SELECT id, repo_id, sequence_number, root_tree_hash, parent_ids,
		author, message, agent_model, session_id, task_trace_id, created_at
		FROM changesets WHERE repo_id = ? AND sequence_number = ? LIMIT 1`, repoID, seq)

	var cs graph.Changeset
	var parentIDsStr string
	var agentModel, sessionID, taskTraceID sql.NullString

	if err := row.Scan(
		&cs.ID, &cs.RepoID, &cs.SequenceNumber, &cs.RootTreeHash, &parentIDsStr,
		&cs.Author, &cs.Message, &agentModel, &sessionID, &taskTraceID, &cs.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("changeset with sequence #%d not found: %w", seq, err)
	}

	if parentIDsStr != "" {
		cs.ParentIDs = strings.Split(parentIDsStr, ",")
	} else {
		cs.ParentIDs = []string{}
	}
	if agentModel.Valid {
		cs.AgentModel = agentModel.String
	}
	if sessionID.Valid {
		cs.SessionID = sessionID.String
	}
	if taskTraceID.Valid {
		cs.TaskTraceID = taskTraceID.String
	}

	return &cs, nil
}


func (d *DB) GetChangesetsByRepo(repoID string, limit int) ([]graph.Changeset, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.sqlDB.Query(`SELECT id, repo_id, sequence_number, root_tree_hash, parent_ids,
		author, message, agent_model, session_id, task_trace_id, created_at
		FROM changesets WHERE repo_id = ? ORDER BY sequence_number DESC, created_at DESC LIMIT ?`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []graph.Changeset
	for rows.Next() {
		var cs graph.Changeset
		var parentIDsStr string
		var agentModel, sessionID, taskTraceID sql.NullString

		if err := rows.Scan(
			&cs.ID, &cs.RepoID, &cs.SequenceNumber, &cs.RootTreeHash, &parentIDsStr,
			&cs.Author, &cs.Message, &agentModel, &sessionID, &taskTraceID, &cs.CreatedAt,
		); err != nil {
			return nil, err
		}

		if parentIDsStr != "" {
			cs.ParentIDs = strings.Split(parentIDsStr, ",")
		} else {
			cs.ParentIDs = []string{}
		}
		if agentModel.Valid {
			cs.AgentModel = agentModel.String
		}
		if sessionID.Valid {
			cs.SessionID = sessionID.String
		}
		if taskTraceID.Valid {
			cs.TaskTraceID = taskTraceID.String
		}

		result = append(result, cs)
	}
	return result, nil
}

func (d *DB) SaveSparseProfile(profile *SparseProfile) error {
	data, err := json.Marshal(profile.IncludedPatterns)
	if err != nil {
		return err
	}
	_, err = d.sqlDB.Exec(`INSERT OR REPLACE INTO sparse_profiles (id, repo_id, name, included_patterns, created_at)
		VALUES (?, ?, ?, ?, ?)`, profile.ID, profile.RepoID, profile.Name, string(data), profile.CreatedAt)
	return err
}

func (d *DB) GetSparseProfiles(repoID string) ([]SparseProfile, error) {
	rows, err := d.sqlDB.Query(`SELECT id, repo_id, name, included_patterns, created_at FROM sparse_profiles WHERE repo_id = ?`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []SparseProfile
	for rows.Next() {
		var p SparseProfile
		var patternsRaw string
		if err := rows.Scan(&p.ID, &p.RepoID, &p.Name, &patternsRaw, &p.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(patternsRaw), &p.IncludedPatterns)
		profiles = append(profiles, p)
	}
	return profiles, nil
}
