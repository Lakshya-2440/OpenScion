package vfs

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/tree"
)

func setupVFSTest(t *testing.T) (*MerkleFS, *cas.Store) {
	tempDir := t.TempDir()
	store, err := cas.NewStore(filepath.Join(tempDir, "cas"))
	if err != nil {
		t.Fatalf("failed to create CAS: %v", err)
	}

	tr := tree.NewTree()

	// Add files in various directories
	f1Data := []byte("root file content\n")
	m1, _ := store.PutBlob(f1Data)
	tr.Entries["README.md"] = tree.Entry{Path: "README.md", Type: tree.TypeBlob, Hash: m1.BlobHash, Size: m1.TotalSize, Mode: 0644}

	f2Data := []byte("package main\nfunc main() {}\n")
	m2, _ := store.PutBlob(f2Data)
	tr.Entries["src/main.go"] = tree.Entry{Path: "src/main.go", Type: tree.TypeBlob, Hash: m2.BlobHash, Size: m2.TotalSize, Mode: 0644}

	f3Data := []byte("package utils\nfunc Helper() {}\n")
	m3, _ := store.PutBlob(f3Data)
	tr.Entries["src/pkg/utils/helper.go"] = tree.Entry{Path: "src/pkg/utils/helper.go", Type: tree.TypeBlob, Hash: m3.BlobHash, Size: m3.TotalSize, Mode: 0644}

	tr.ComputeHash()

	opts := MountOptions{
		CacheDir:   filepath.Join(tempDir, "vfs_cache"),
		MaxCacheMB: 10,
	}

	fs := NewMerkleFS(tr, store, time.Now(), opts)
	return fs, store
}

func TestVFS_StatDirectoryAndFiles(t *testing.T) {
	fs, _ := setupVFSTest(t)
	ctx := context.Background()

	// Root stat
	rootInfo, err := fs.Stat(ctx, "/")
	if err != nil {
		t.Fatalf("failed to stat root: %v", err)
	}
	if !rootInfo.IsDir() {
		t.Errorf("expected root to be dir")
	}

	// Subdirectory stat
	srcInfo, err := fs.Stat(ctx, "/src")
	if err != nil {
		t.Fatalf("failed to stat /src: %v", err)
	}
	if !srcInfo.IsDir() {
		t.Errorf("expected /src to be dir")
	}

	// Deep directory stat
	deepInfo, err := fs.Stat(ctx, "/src/pkg/utils")
	if err != nil {
		t.Fatalf("failed to stat /src/pkg/utils: %v", err)
	}
	if !deepInfo.IsDir() {
		t.Errorf("expected /src/pkg/utils to be dir")
	}

	// File stat
	fileInfo, err := fs.Stat(ctx, "/src/main.go")
	if err != nil {
		t.Fatalf("failed to stat /src/main.go: %v", err)
	}
	if fileInfo.IsDir() {
		t.Errorf("expected /src/main.go to be file")
	}
	if fileInfo.Size() <= 0 {
		t.Errorf("expected positive file size, got %d", fileInfo.Size())
	}
}

func TestVFS_LazyHydrationAndStats(t *testing.T) {
	fs, _ := setupVFSTest(t)
	ctx := context.Background()

	// 1. Prior to opening any file, verified 0 files and 0 bytes hydrated
	hydratedCount, bytesServed := fs.Stats()
	if hydratedCount != 0 || bytesServed != 0 {
		t.Fatalf("expected 0 hydrated files initially, got %d files, %d bytes", hydratedCount, bytesServed)
	}

	// 2. Open and read README.md
	f, err := fs.OpenFile(ctx, "/README.md", 0, 0644)
	if err != nil {
		t.Fatalf("failed to open README.md: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}
	if string(content) != "root file content\n" {
		t.Errorf("unexpected content: %q", string(content))
	}

	// 3. Stats must now reflect exactly 1 file hydrated
	hydratedCount, bytesServed = fs.Stats()
	if hydratedCount != 1 || bytesServed != int64(len(content)) {
		t.Fatalf("expected 1 file and %d bytes hydrated, got %d files, %d bytes", len(content), hydratedCount, bytesServed)
	}

	// 4. Repeated read of same file should use cache without increasing hydratedCount
	f2, err := fs.OpenFile(ctx, "/README.md", 0, 0644)
	if err != nil {
		t.Fatalf("failed to open README.md second time: %v", err)
	}
	defer f2.Close()
	content2, _ := io.ReadAll(f2)
	if string(content2) != string(content) {
		t.Errorf("content mismatch on cached read")
	}

	hydratedCount2, _ := fs.Stats()
	if hydratedCount2 != 1 {
		t.Fatalf("expected hydratedCount to stay 1, got %d", hydratedCount2)
	}
}

func TestVFS_SeekAndPartialRead(t *testing.T) {
	fs, _ := setupVFSTest(t)
	ctx := context.Background()

	f, err := fs.OpenFile(ctx, "/src/main.go", 0, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer f.Close()

	// Seek to offset 8 (" main...")
	offset, err := f.Seek(8, io.SeekStart)
	if err != nil || offset != 8 {
		t.Fatalf("seek failed: %v", err)
	}

	buf := make([]byte, 4)
	n, err := f.Read(buf)
	if err != nil || n != 4 {
		t.Fatalf("read after seek failed: %v", err)
	}
	if string(buf) != "main" {
		t.Errorf("expected 'main', got %q", string(buf))
	}
}

func TestVFS_DirectoryReaddir(t *testing.T) {
	fs, _ := setupVFSTest(t)
	ctx := context.Background()

	dirHandle, err := fs.OpenFile(ctx, "/src", 0, 0755)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirHandle.Close()

	entries, err := dirHandle.Readdir(-1)
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = true
	}

	if !names["main.go"] {
		t.Errorf("expected main.go in /src")
	}
	if !names["pkg"] {
		t.Errorf("expected pkg/ in /src")
	}
}

func TestVFS_ConcurrentReads(t *testing.T) {
	fs, _ := setupVFSTest(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	numWorkers := 30
	errors := make([]error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			path := "/src/pkg/utils/helper.go"
			if workerIdx%2 == 0 {
				path = "/src/main.go"
			}

			f, err := fs.OpenFile(ctx, path, 0, 0644)
			if err != nil {
				errors[workerIdx] = fmt.Errorf("open failed: %w", err)
				return
			}
			defer f.Close()

			data, err := io.ReadAll(f)
			if err != nil {
				errors[workerIdx] = fmt.Errorf("read failed: %w", err)
				return
			}
			if len(data) == 0 {
				errors[workerIdx] = fmt.Errorf("read 0 bytes")
				return
			}
		}(i)
	}

	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Fatalf("worker %d failed: %v", i, err)
		}
	}
}
