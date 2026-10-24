package db

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDB_SparseProfilesAndRebind(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// 1. Test Save & Get Sparse Profile in SQLite
	prof := &SparseProfile{
		ID:               "prof-1",
		RepoID:           "repo-1",
		Name:             "test-profile",
		IncludedPatterns: []string{"src/*", "docs/*"},
		CreatedAt:        time.Now().Unix(),
	}
	err = database.SaveSparseProfile(prof)
	if err != nil {
		t.Fatalf("SaveSparseProfile failed: %v", err)
	}

	profiles, err := database.GetSparseProfiles("repo-1")
	if err != nil {
		t.Fatalf("GetSparseProfiles failed: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != "prof-1" {
		t.Fatalf("unexpected profiles: %v", profiles)
	}
	if len(profiles[0].IncludedPatterns) != 2 || profiles[0].IncludedPatterns[0] != "src/*" {
		t.Errorf("unexpected patterns: %v", profiles[0].IncludedPatterns)
	}

	// 2. Test rebind logic for Postgres
	database.isPostgres = true

	qTrees := database.rebind("INSERT OR IGNORE INTO trees (hash, data) VALUES (?, ?)")
	if !strings.Contains(qTrees, "ON CONFLICT (hash) DO NOTHING") || !strings.Contains(qTrees, "$1, $2") {
		t.Errorf("unexpected rebind for trees: %s", qTrees)
	}

	qProfile := database.rebind("INSERT OR REPLACE INTO sparse_profiles (id, repo_id, name, included_patterns, created_at) VALUES (?, ?, ?, ?, ?)")
	if !strings.Contains(qProfile, "ON CONFLICT (id) DO UPDATE SET included_patterns = EXCLUDED.included_patterns") {
		t.Errorf("unexpected rebind for sparse_profiles: %s", qProfile)
	}
	if !strings.Contains(qProfile, "$1, $2, $3, $4, $5") {
		t.Errorf("expected $1, $2, $3, $4, $5 in rebind, got: %s", qProfile)
	}
}
