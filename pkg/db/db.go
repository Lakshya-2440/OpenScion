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
	_ "github.com/lib/pq"
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
	sqlDB      *sql.DB
	isPostgres bool
	mu         sync.Mutex
}

// Open initializes the database connection (SQLite or PostgreSQL / CockroachDB)
func Open(dataSource string) (*DB, error) {
	var sqlDB *sql.DB
	var isPostgres bool
	var err error

	if strings.HasPrefix(dataSource, "postgres://") || strings.HasPrefix(dataSource, "postgresql://") {
		isPostgres = true
		sqlDB, err = sql.Open("postgres", dataSource)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to PostgreSQL/CockroachDB: %w", err)
		}
		sqlDB.SetMaxOpenConns(50)
		sqlDB.SetMaxIdleConns(25)
	} else {
		if !strings.Contains(dataSource, "_pragma=busy_timeout") && !strings.Contains(dataSource, "_busy_timeout") {
			if strings.Contains(dataSource, "?") {
				dataSource += "&_pragma=busy_timeout(10000)"
			} else {
				dataSource += "?_pragma=busy_timeout(10000)"
			}
		}
		if !strings.Contains(dataSource, ":memory:") && !strings.Contains(dataSource, "mode=memory") && !strings.Contains(dataSource, "_pragma=journal_mode") {
			if strings.Contains(dataSource, "?") {
				dataSource += "&_pragma=journal_mode(wal)"
			} else {
				dataSource += "?_pragma=journal_mode(wal)"
			}
		}
		sqlDB, err = sql.Open("sqlite", dataSource)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(25)
	}

	d := &DB{
		sqlDB:      sqlDB,
		isPostgres: isPostgres,
	}
	if err := d.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	return d, nil
}

func (d *DB) rebind(query string) string {
	if !d.isPostgres {
		return query
	}

	// Handle SQLite specific syntax conversion
	query = strings.ReplaceAll(query, "INSERT OR IGNORE INTO trees (hash, data) VALUES (?, ?)", "INSERT INTO trees (hash, data) VALUES (?, ?) ON CONFLICT (hash) DO NOTHING")
	query = strings.ReplaceAll(query, "INSERT OR REPLACE INTO trees (hash, data) VALUES (?, ?)", "INSERT INTO trees (hash, data) VALUES (?, ?) ON CONFLICT (hash) DO UPDATE SET data = EXCLUDED.data")
	query = strings.ReplaceAll(query, "INSERT OR REPLACE INTO changesets (", "INSERT INTO changesets (")
	if strings.Contains(query, "INSERT INTO changesets (") && !strings.Contains(query, "ON CONFLICT") {
		query += " ON CONFLICT (id) DO UPDATE SET sequence_number = EXCLUDED.sequence_number, root_tree_hash = EXCLUDED.root_tree_hash"
	}
	query = strings.ReplaceAll(query, "INSERT OR REPLACE INTO sparse_profiles (", "INSERT INTO sparse_profiles (")
	if strings.Contains(query, "INSERT INTO sparse_profiles (") && !strings.Contains(query, "ON CONFLICT") {
		query += " ON CONFLICT (id) DO UPDATE SET included_patterns = EXCLUDED.included_patterns"
	}

	var b strings.Builder
	paramIdx := 1
	for _, ch := range query {
		if ch == '?' {
			b.WriteString(fmt.Sprintf("$%d", paramIdx))
			paramIdx++
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func (d *DB) exec(query string, args ...any) (sql.Result, error) {
	reb := d.rebind(query)
	var lastErr error
	for attempt := 0; attempt < 40; attempt++ {
		res, err := d.sqlDB.Exec(reb, args...)
		if err == nil {
			return res, nil
		}
		lastErr = err
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "locked") || strings.Contains(errText, "busy") {
			delay := 10 * time.Millisecond * time.Duration(attempt+1)
			if delay > 250*time.Millisecond {
				delay = 250 * time.Millisecond
			}
			time.Sleep(delay)
			continue
		}
		return nil, err
	}
	return nil, lastErr
}

func (d *DB) queryRow(query string, args ...any) *sql.Row {
	return d.sqlDB.QueryRow(d.rebind(query), args...)
}

func (d *DB) query(query string, args ...any) (*sql.Rows, error) {
	return d.sqlDB.Query(d.rebind(query), args...)
}

func (d *DB) execTx(tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return tx.Exec(d.rebind(query), args...)
}

func (d *DB) Close() error {
	return d.sqlDB.Close()
}

func (d *DB) initSchema() error {
	queries := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA synchronous=NORMAL;`,
		`PRAGMA busy_timeout=10000;`,
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
		`CREATE TABLE IF NOT EXISTS stashes (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			branch TEXT NOT NULL,
			base_changeset_id TEXT NOT NULL,
			tree_hash TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_stashes_repo_created ON stashes(repo_id, created_at DESC);`,
	}

	for _, q := range queries {
		if d.isPostgres && strings.HasPrefix(q, "PRAGMA") {
			continue
		}
		if _, err := d.sqlDB.Exec(d.rebind(q)); err != nil {
			return fmt.Errorf("error executing %q: %w", q, err)
		}
	}
	return nil
}

// AlignRepoID synchronizes local repo_id across repositories, branches, and changesets
func (d *DB) AlignRepoID(oldID, newID string) error {
	if oldID == newID || oldID == "" || newID == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.sqlDB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, _ = d.execTx(tx, `UPDATE repositories SET id = ? WHERE id = ?`, newID, oldID)
	_, _ = d.execTx(tx, `UPDATE branches SET repo_id = ? WHERE repo_id = ?`, newID, oldID)
	_, _ = d.execTx(tx, `UPDATE changesets SET repo_id = ? WHERE repo_id = ?`, newID, oldID)

	return tx.Commit()
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

	_, err = d.execTx(tx, `INSERT INTO repositories (id, name, created_at) VALUES (?, ?, ?)`,
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
	_, err = d.execTx(tx, `INSERT OR IGNORE INTO trees (hash, data) VALUES (?, ?)`, emptyTreeHash, string(data))
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

	_, err = d.execTx(tx, `INSERT INTO changesets (
		id, repo_id, sequence_number, root_tree_hash, parent_ids,
		author, message, agent_model, session_id, task_trace_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		initialCS.ID, repoID, initialCS.SequenceNumber, initialCS.RootTreeHash, "",
		initialCS.Author, initialCS.Message, initialCS.AgentModel, initialCS.SessionID, initialCS.TaskTraceID, initialCS.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Set main branch
	_, err = d.execTx(tx, `INSERT INTO branches (repo_id, name, head_changeset_id, sequence_number, updated_at)
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
	row := d.queryRow(`SELECT id, name, created_at FROM repositories WHERE name = ? OR id = ?`, name, name)
	var r Repository
	if err := row.Scan(&r.ID, &r.Name, &r.CreatedAt); err != nil {
		return nil, fmt.Errorf("repository %q not found: %w", name, err)
	}
	return &r, nil
}

func (d *DB) ListRepos() ([]Repository, error) {
	rows, err := d.query(`SELECT id, name, created_at FROM repositories ORDER BY created_at DESC`)
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
	row := d.queryRow(`SELECT repo_id, name, head_changeset_id, sequence_number, updated_at
		FROM branches WHERE repo_id = ? AND name = ?`, repoID, branchName)
	var b Branch
	if err := row.Scan(&b.RepoID, &b.Name, &b.HeadID, &b.SequenceNumber, &b.UpdatedAt); err != nil {
		return nil, fmt.Errorf("branch %q not found for repo %s: %w", branchName, repoID, err)
	}
	return &b, nil
}

func (d *DB) ListBranches(repoID string) ([]Branch, error) {
	rows, err := d.query(`SELECT repo_id, name, head_changeset_id, sequence_number, updated_at
		FROM branches WHERE repo_id = ? ORDER BY name ASC`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var branches []Branch
	for rows.Next() {
		var b Branch
		if err := rows.Scan(&b.RepoID, &b.Name, &b.HeadID, &b.SequenceNumber, &b.UpdatedAt); err == nil {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

func (d *DB) CreateBranch(repoID, branchName, headID string, seq int64) error {
	now := time.Now().Unix()
	_, err := d.exec(`INSERT INTO branches (repo_id, name, head_changeset_id, sequence_number, updated_at)
		VALUES (?, ?, ?, ?, ?)`, repoID, branchName, headID, seq, now)
	return err
}

func (d *DB) DeleteBranch(repoID, branchName string) error {
	_, err := d.exec(`DELETE FROM branches WHERE repo_id = ? AND name = ?`, repoID, branchName)
	return err
}

func (d *DB) UpdateBranchTip(repoID, branchName, newHeadID string, newSeq int64) error {
	now := time.Now().Unix()
	res, err := d.exec(`UPDATE branches SET head_changeset_id = ?, sequence_number = ?, updated_at = ?
		WHERE repo_id = ? AND name = ?`, newHeadID, newSeq, now, repoID, branchName)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		_, err = d.exec(`INSERT INTO branches (repo_id, name, head_changeset_id, sequence_number, updated_at)
			VALUES (?, ?, ?, ?, ?)`, repoID, branchName, newHeadID, newSeq, now)
		return err
	}
	return nil
}

// CompareAndSwapBranchTip atomically updates a branch tip only if current head and sequence match expected
func (d *DB) CompareAndSwapBranchTip(repoID, branchName, expectedHeadID string, expectedSeq int64, newHeadID string, newSeq int64) (bool, error) {
	now := time.Now().Unix()
	res, err := d.exec(`UPDATE branches SET head_changeset_id = ?, sequence_number = ?, updated_at = ?
		WHERE repo_id = ? AND name = ? AND head_changeset_id = ? AND sequence_number = ?`,
		newHeadID, newSeq, now, repoID, branchName, expectedHeadID, expectedSeq)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (d *DB) SaveTree(t *tree.Tree) error {
	data, err := t.Serialize()
	if err != nil {
		return err
	}
	_, err = d.exec(`INSERT OR REPLACE INTO trees (hash, data) VALUES (?, ?)`, t.RootHash, string(data))
	return err
}

func (d *DB) GetTree(hash string) (*tree.Tree, error) {
	row := d.queryRow(`SELECT data FROM trees WHERE hash = ?`, hash)
	var raw string
	if err := row.Scan(&raw); err != nil {
		return nil, fmt.Errorf("tree hash %s not found: %w", hash, err)
	}
	return tree.Deserialize([]byte(raw))
}

func (d *DB) SaveChangeset(cs *graph.Changeset) error {
	parentIDsStr := strings.Join(cs.ParentIDs, ",")
	_, err := d.exec(`INSERT OR REPLACE INTO changesets (
		id, repo_id, sequence_number, root_tree_hash, parent_ids,
		author, message, agent_model, session_id, task_trace_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cs.ID, cs.RepoID, cs.SequenceNumber, cs.RootTreeHash, parentIDsStr,
		cs.Author, cs.Message, cs.AgentModel, cs.SessionID, cs.TaskTraceID, cs.CreatedAt)
	return err
}

func (d *DB) GetChangeset(id string) (*graph.Changeset, error) {
	row := d.queryRow(`SELECT id, repo_id, sequence_number, root_tree_hash, parent_ids,
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
	row := d.queryRow(`SELECT id, repo_id, sequence_number, root_tree_hash, parent_ids,
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
	rows, err := d.query(`SELECT id, repo_id, sequence_number, root_tree_hash, parent_ids,
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
	_, err = d.exec(`INSERT OR REPLACE INTO sparse_profiles (id, repo_id, name, included_patterns, created_at)
		VALUES (?, ?, ?, ?, ?)`, profile.ID, profile.RepoID, profile.Name, string(data), profile.CreatedAt)
	return err
}

func (d *DB) GetSparseProfiles(repoID string) ([]SparseProfile, error) {
	rows, err := d.query(`SELECT id, repo_id, name, included_patterns, created_at FROM sparse_profiles WHERE repo_id = ?`, repoID)
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

type MergeQueueEntry struct {
	ID                string `json:"id"`
	RepoID            string `json:"repo_id"`
	ChangesetID       string `json:"changeset_id"`
	Status            string `json:"status"`
	SpeculativeParent string `json:"speculative_parent,omitempty"`
	EnqueuedAt        int64  `json:"enqueued_at"`
	LandedAt          int64  `json:"landed_at,omitempty"`
}

func (d *DB) EnqueueMerge(id, repoID, changesetID, speculativeParent string) error {
	now := time.Now().Unix()
	_, err := d.exec(`INSERT INTO merge_queue (id, repo_id, changeset_id, status, speculative_parent, enqueued_at)
		VALUES (?, ?, ?, 'ENQUEUED', ?, ?)`, id, repoID, changesetID, speculativeParent, now)
	return err
}

func (d *DB) UpdateMergeQueueStatus(id string, status string, landedAt int64) error {
	_, err := d.exec(`UPDATE merge_queue SET status = ?, landed_at = ? WHERE id = ?`, status, landedAt, id)
	return err
}

func (d *DB) GetMergeQueueEntry(id string) (*MergeQueueEntry, error) {
	row := d.queryRow(`SELECT id, repo_id, changeset_id, status, speculative_parent, enqueued_at, landed_at
		FROM merge_queue WHERE id = ?`, id)
	var e MergeQueueEntry
	var specParent sql.NullString
	var landedAt sql.NullInt64
	if err := row.Scan(&e.ID, &e.RepoID, &e.ChangesetID, &e.Status, &specParent, &e.EnqueuedAt, &landedAt); err != nil {
		return nil, err
	}
	if specParent.Valid {
		e.SpeculativeParent = specParent.String
	}
	if landedAt.Valid {
		e.LandedAt = landedAt.Int64
	}
	return &e, nil
}

type Stash struct {
	ID              string `json:"id"`
	RepoID          string `json:"repo_id"`
	Branch          string `json:"branch"`
	BaseChangesetID string `json:"base_changeset_id"`
	TreeHash        string `json:"tree_hash"`
	Message         string `json:"message"`
	CreatedAt       int64  `json:"created_at"`
}

func (d *DB) SaveStash(s *Stash) error {
	_, err := d.exec(`INSERT INTO stashes (id, repo_id, branch, base_changeset_id, tree_hash, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, s.ID, s.RepoID, s.Branch, s.BaseChangesetID, s.TreeHash, s.Message, s.CreatedAt)
	return err
}

func (d *DB) ListStashes(repoID string) ([]*Stash, error) {
	rows, err := d.query(`SELECT id, repo_id, branch, base_changeset_id, tree_hash, message, created_at
		FROM stashes WHERE repo_id = ? ORDER BY created_at DESC`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stashes []*Stash
	for rows.Next() {
		var s Stash
		if err := rows.Scan(&s.ID, &s.RepoID, &s.Branch, &s.BaseChangesetID, &s.TreeHash, &s.Message, &s.CreatedAt); err != nil {
			return nil, err
		}
		stashes = append(stashes, &s)
	}
	return stashes, nil
}

func (d *DB) GetStash(id string) (*Stash, error) {
	row := d.queryRow(`SELECT id, repo_id, branch, base_changeset_id, tree_hash, message, created_at
		FROM stashes WHERE id = ?`, id)
	var s Stash
	if err := row.Scan(&s.ID, &s.RepoID, &s.Branch, &s.BaseChangesetID, &s.TreeHash, &s.Message, &s.CreatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *DB) DeleteStash(id string) error {
	_, err := d.exec(`DELETE FROM stashes WHERE id = ?`, id)
	return err
}
