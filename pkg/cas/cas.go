package cas

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// ChunkSize defines target chunking size (32 KB for content-defined chunking approximation)
const ChunkSize = 32 * 1024

// BlobChunk represents an individual compressed content chunk
type BlobChunk struct {
	Hash           string `json:"hash"`
	UncompressedSz int    `json:"uncompressed_size"`
	Data           []byte `json:"data"` // gzip-compressed payload
}

// BlobManifest maps a logical file blob to its constituent chunks
type BlobManifest struct {
	BlobHash       string   `json:"blob_hash"`
	TotalSize      int64    `json:"total_size"`
	ChunkHashes    []string `json:"chunk_hashes"`
	IsSingleChunk  bool     `json:"is_single_chunk"`
}

// Store provides content-addressable storage on local disk
type Store struct {
	baseDir string
	mu      sync.RWMutex
}

// NewStore initializes a CAS directory
func NewStore(baseDir string) (*Store, error) {
	objDir := filepath.Join(baseDir, "objects")
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create CAS objects dir: %w", err)
	}
	return &Store{baseDir: baseDir}, nil
}

func HashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// chunkPath returns path on disk for given chunk hash: objects/ab/cd/abcdef...chunk
func (s *Store) chunkPath(hash string) string {
	if len(hash) < 4 {
		return filepath.Join(s.baseDir, "objects", hash+".chunk")
	}
	return filepath.Join(s.baseDir, "objects", hash[0:2], hash[2:4], hash+".chunk")
}

// PutChunk stores a compressed chunk if not already present
func (s *Store) PutChunk(chunk *BlobChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetPath := s.chunkPath(chunk.Hash)
	if _, err := os.Stat(targetPath); err == nil {
		// Already stored, deduplicated
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	tmpFile := targetPath + ".tmp"
	if err := os.WriteFile(tmpFile, chunk.Data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpFile, targetPath)
}

// GetChunk retrieves and returns the raw compressed chunk
func (s *Store) GetChunk(hash string) (*BlobChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targetPath := s.chunkPath(hash)
	compressedData, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("chunk not found: %s: %w", hash, err)
	}

	return &BlobChunk{
		Hash: hash,
		Data: compressedData,
	}, nil
}

// PutBlob processes raw content: chunks it, compresses each chunk, stores chunks, and returns manifest
func (s *Store) PutBlob(content []byte) (*BlobManifest, error) {
	fullHash := HashBytes(content)
	totalLen := int64(len(content))

	// For small files, single chunk
	if len(content) <= ChunkSize {
		chunk, err := compressChunk(fullHash, content)
		if err != nil {
			return nil, err
		}
		if err := s.PutChunk(chunk); err != nil {
			return nil, err
		}

		manifest := &BlobManifest{
			BlobHash:      fullHash,
			TotalSize:     totalLen,
			ChunkHashes:   []string{fullHash},
			IsSingleChunk: true,
		}
		if err := s.saveManifest(manifest); err != nil {
			return nil, err
		}
		return manifest, nil
	}

	// For larger files: slice into chunks
	var chunkHashes []string
	for offset := 0; offset < len(content); offset += ChunkSize {
		end := offset + ChunkSize
		if end > len(content) {
			end = len(content)
		}
		slice := content[offset:end]
		cHash := HashBytes(slice)
		chunk, err := compressChunk(cHash, slice)
		if err != nil {
			return nil, err
		}
		if err := s.PutChunk(chunk); err != nil {
			return nil, err
		}
		chunkHashes = append(chunkHashes, cHash)
	}

	manifest := &BlobManifest{
		BlobHash:      fullHash,
		TotalSize:     totalLen,
		ChunkHashes:   chunkHashes,
		IsSingleChunk: false,
	}

	if err := s.saveManifest(manifest); err != nil {
		return nil, err
	}

	return manifest, nil
}

// GetBlob reconstructs full raw content from its manifest
func (s *Store) GetBlob(manifest *BlobManifest) ([]byte, error) {
	var buf bytes.Buffer
	for _, cHash := range manifest.ChunkHashes {
		chunk, err := s.GetChunk(cHash)
		if err != nil {
			return nil, err
		}
		raw, err := decompressChunk(chunk.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress chunk %s: %w", cHash, err)
		}
		buf.Write(raw)
	}
	return buf.Bytes(), nil
}

// GetBlobByHash reads manifest by blob hash and returns reconstructed bytes
func (s *Store) GetBlobByHash(blobHash string) ([]byte, error) {
	manifest, err := s.GetManifest(blobHash)
	if err != nil {
		return nil, err
	}
	return s.GetBlob(manifest)
}

func (s *Store) manifestPath(blobHash string) string {
	if len(blobHash) < 4 {
		return filepath.Join(s.baseDir, "manifests", blobHash+".json")
	}
	return filepath.Join(s.baseDir, "manifests", blobHash[0:2], blobHash[2:4], blobHash+".json")
}

func (s *Store) saveManifest(m *BlobManifest) error {
	mPath := s.manifestPath(m.BlobHash)
	if err := os.MkdirAll(filepath.Dir(mPath), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(mPath, data, 0644)
}

func (s *Store) GetManifest(blobHash string) (*BlobManifest, error) {
	mPath := s.manifestPath(blobHash)
	data, err := os.ReadFile(mPath)
	if err != nil {
		return nil, fmt.Errorf("manifest not found for blob %s: %w", blobHash, err)
	}
	var m BlobManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func compressChunk(hash string, raw []byte) (*BlobChunk, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &BlobChunk{
		Hash:           hash,
		UncompressedSz: len(raw),
		Data:           buf.Bytes(),
	}, nil
}

func decompressChunk(compressed []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}
