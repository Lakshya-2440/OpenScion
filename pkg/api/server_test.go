package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
)

func setupTestServer(t *testing.T, authToken string) (*Server, func()) {
	tmpDir := t.TempDir()
	database, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	casStore, err := cas.NewStore(filepath.Join(tmpDir, "cas"))
	if err != nil {
		t.Fatalf("failed to init cas: %v", err)
	}

	server := NewServer(database, casStore)
	if authToken != "" {
		server.SetAuthToken(authToken)
	}

	cleanup := func() {
		database.Close()
	}
	return server, cleanup
}

func TestServerAuth(t *testing.T) {
	server, cleanup := setupTestServer(t, "secret-token-123")
	defer cleanup()

	// 1. Unauthenticated request to /api/v1/repos/list
	req := httptest.NewRequest("GET", "/api/v1/repos/list", nil)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rr.Code)
	}

	// 2. Invalid Bearer token
	req = httptest.NewRequest("GET", "/api/v1/repos/list", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rr.Code)
	}

	// 3. Valid Bearer token
	req = httptest.NewRequest("GET", "/api/v1/repos/list", nil)
	req.Header.Set("Authorization", "Bearer secret-token-123")
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with valid token, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestServerBatchManifests(t *testing.T) {
	server, cleanup := setupTestServer(t, "")
	defer cleanup()

	// Create test manifest
	manifest := &cas.BlobManifest{
		BlobHash:    "manifest-hash-1",
		TotalSize:   128,
		ChunkHashes: []string{"chunk-hash-1"},
	}

	body, err := json.Marshal([]*cas.BlobManifest{manifest})
	if err != nil {
		t.Fatalf("failed to marshal manifests: %v", err)
	}

	// POST /api/v1/cas/manifests/batch-put
	req := httptest.NewRequest("POST", "/api/v1/cas/manifests/batch-put", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("batch manifest upload failed: %d %s", rr.Code, rr.Body.String())
	}

	// Retrieve manifest via CAS store directly to verify persistence
	retrieved, err := server.store.GetManifest("manifest-hash-1")
	if err != nil {
		t.Fatalf("failed to retrieve uploaded manifest: %v", err)
	}
	if retrieved.TotalSize != 128 || len(retrieved.ChunkHashes) != 1 {
		t.Errorf("retrieved manifest does not match: %+v", retrieved)
	}
}
