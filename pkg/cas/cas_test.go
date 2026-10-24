package cas

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCAS_PutBlobFromFile_Equivalence(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	// Create test file larger than ChunkSize (e.g. 70 KB)
	largeData := bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz0123456789\n"), 2000)
	filePath := filepath.Join(tmpDir, "test_large.dat")
	if err := os.WriteFile(filePath, largeData, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	mStream, err := store.PutBlobFromFile(filePath)
	if err != nil {
		t.Fatalf("PutBlobFromFile failed: %v", err)
	}

	mDirect, err := store.PutBlob(largeData)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	if mStream.BlobHash != mDirect.BlobHash {
		t.Errorf("hash mismatch: stream %s vs direct %s", mStream.BlobHash, mDirect.BlobHash)
	}
	if len(mStream.ChunkHashes) != len(mDirect.ChunkHashes) {
		t.Errorf("chunk count mismatch: stream %d vs direct %d", len(mStream.ChunkHashes), len(mDirect.ChunkHashes))
	}

	// Reconstruct blob and verify content
	retrieved, err := store.GetBlob(mStream)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	if !bytes.Equal(retrieved, largeData) {
		t.Errorf("retrieved data differs from original content")
	}
}

func TestCAS_BitRotCorruptionDetection(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	data := []byte("important content that will suffer bit rot")
	manifest, err := store.PutBlob(data)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	chunkHash := manifest.ChunkHashes[0]
	chunkPath := store.chunkPath(chunkHash)

	// Tamper with the chunk on disk (corrupt byte payload)
	if err := os.WriteFile(chunkPath, []byte("corrupted invalid gzip data"), 0644); err != nil {
		t.Fatalf("failed to overwrite chunk: %v", err)
	}

	// GetBlob should detect corruption and return an error
	_, err = store.GetBlob(manifest)
	if err == nil {
		t.Fatalf("expected error on corrupted chunk, got nil")
	}
}

func TestCAS_GC(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	m1, _ := store.PutBlob([]byte("reachable blob"))
	m2, _ := store.PutBlob([]byte("unreachable orphan blob"))

	reachable := map[string]bool{
		m1.BlobHash: true,
	}
	for _, ch := range m1.ChunkHashes {
		reachable[ch] = true
	}

	deleted, freed, err := store.GC(reachable)
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}
	if deleted == 0 || freed == 0 {
		t.Errorf("expected GC to delete unreferenced chunks, deleted: %d, freed: %d", deleted, freed)
	}

	// m1 should be intact
	if _, err := store.GetBlob(m1); err != nil {
		t.Errorf("reachable blob m1 should not be deleted: %v", err)
	}

	// m2 should be deleted
	if _, err := store.GetBlob(m2); err == nil {
		t.Errorf("expected error getting deleted orphan blob m2")
	}
}
