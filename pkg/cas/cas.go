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
	"strings"
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
	BlobHash      string   `json:"blob_hash"`
	TotalSize     int64    `json:"total_size"`
	ChunkHashes   []string `json:"chunk_hashes"`
	IsSingleChunk bool     `json:"is_single_chunk"`
}

func ValidateChunk(chunk *BlobChunk) error {
	if chunk == nil || chunk.Hash == "" {
		return fmt.Errorf("chunk hash is required")
	}
	raw, err := decompressChunk(chunk.Data)
	if err != nil {
		return fmt.Errorf("invalid compressed chunk %s: %w", chunk.Hash, err)
	}
	if chunk.UncompressedSz != len(raw) {
		return fmt.Errorf("chunk %s size mismatch: declared %d, got %d", chunk.Hash, chunk.UncompressedSz, len(raw))
	}
	if HashBytes(raw) != chunk.Hash {
		return fmt.Errorf("chunk %s hash mismatch", chunk.Hash)
	}
	return nil
}

func ValidateManifest(m *BlobManifest) error {
	if m == nil || m.BlobHash == "" {
		return fmt.Errorf("blob hash is required")
	}
	for _, hash := range m.ChunkHashes {
		if hash == "" {
			return fmt.Errorf("manifest %s contains an empty chunk hash", m.BlobHash)
		}
	}
	return nil
}

// Store provides content-addressable storage on local disk
type Store struct {
	baseDir string
	remote  RemoteStorage
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

// SetRemote attaches an S3 or MinIO distributed remote storage tier
func (s *Store) SetRemote(remote RemoteStorage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remote = remote
}

// GetRemote returns attached remote storage tier if any
func (s *Store) GetRemote() RemoteStorage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.remote
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

// PutChunk stores a compressed chunk locally and uploads to S3 if configured
func (s *Store) PutChunk(chunk *BlobChunk) error {
	if err := ValidateChunk(chunk); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	targetPath := s.chunkPath(chunk.Hash)
	if _, err := os.Stat(targetPath); err != nil {
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}
		tmpFile := targetPath + ".tmp"
		if err := os.WriteFile(tmpFile, chunk.Data, 0644); err != nil {
			return err
		}
		_ = os.Rename(tmpFile, targetPath)
	}

	// Upload to S3 / MinIO backend if attached
	if s.remote != nil {
		if err := s.remote.PutChunk(chunk.Hash, chunk.Data); err != nil {
			return fmt.Errorf("failed to replicate chunk %s: %w", chunk.Hash, err)
		}
	}
	return nil
}

// GetChunk retrieves chunk from local disk cache, falling back to S3/MinIO
func (s *Store) GetChunk(hash string) (*BlobChunk, error) {
	s.mu.RLock()
	remoteStore := s.remote
	targetPath := s.chunkPath(hash)
	compressedData, err := os.ReadFile(targetPath)
	s.mu.RUnlock()

	if err == nil {
		raw, decodeErr := decompressChunk(compressedData)
		if decodeErr != nil {
			return nil, decodeErr
		}
		return &BlobChunk{
			Hash:           hash,
			UncompressedSz: len(raw),
			Data:           compressedData,
		}, nil
	}

	// Cache miss on local disk — fetch from distributed S3 / MinIO
	if remoteStore != nil {
		data, s3Err := remoteStore.GetChunk(hash)
		if s3Err == nil {
			// Populate local cache
			s.mu.Lock()
			_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
			_ = os.WriteFile(targetPath, data, 0644)
			s.mu.Unlock()

			raw, decodeErr := decompressChunk(data)
			if decodeErr != nil {
				return nil, decodeErr
			}
			return &BlobChunk{
				Hash:           hash,
				UncompressedSz: len(raw),
				Data:           data,
			}, nil
		}
	}

	return nil, fmt.Errorf("chunk not found: %s: %w", hash, err)
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

// PutBlobFromFile streams content from disk in 32KB chunks, avoiding multi-GB RAM spikes
func (s *Store) PutBlobFromFile(filePath string) (*BlobManifest, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	totalLen := stat.Size()
	hasher := sha256.New()
	var chunkHashes []string

	buf := make([]byte, ChunkSize)
	for {
		n, readErr := io.ReadFull(file, buf)
		if n > 0 {
			slice := buf[:n]
			hasher.Write(slice)
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
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}

	fullHash := hex.EncodeToString(hasher.Sum(nil))
	manifest := &BlobManifest{
		BlobHash:      fullHash,
		TotalSize:     totalLen,
		ChunkHashes:   chunkHashes,
		IsSingleChunk: len(chunkHashes) <= 1,
	}

	if err := s.SaveManifest(manifest); err != nil {
		return nil, err
	}

	return manifest, nil
}

// GetBlob reconstructs full raw content from its manifest with bit-rot integrity checking
func (s *Store) GetBlob(manifest *BlobManifest) ([]byte, error) {
	var buf bytes.Buffer
	for _, cHash := range manifest.ChunkHashes {
		chunk, err := s.GetChunk(cHash)
		if err != nil {
			return nil, err
		}
		raw, err := decompressChunk(chunk.Data)
		if err != nil || HashBytes(raw) != cHash {
			// Corrupted chunk detected on disk: remove corrupt chunk and attempt recovery from remote
			_ = os.Remove(s.chunkPath(cHash))
			if s.remote != nil {
				if remoteData, s3Err := s.remote.GetChunk(cHash); s3Err == nil {
					if rawRemote, decErr := decompressChunk(remoteData); decErr == nil && HashBytes(rawRemote) == cHash {
						_ = s.PutChunk(&BlobChunk{Hash: cHash, Data: remoteData})
						buf.Write(rawRemote)
						continue
					}
				}
			}
			if err != nil {
				return nil, fmt.Errorf("failed to decompress chunk %s: %w", cHash, err)
			}
			return nil, fmt.Errorf("corrupted chunk %s: hash mismatch", cHash)
		}
		buf.Write(raw)
	}
	content := buf.Bytes()
	if int64(len(content)) != manifest.TotalSize || HashBytes(content) != manifest.BlobHash {
		return nil, fmt.Errorf("blob %s integrity check failed", manifest.BlobHash)
	}
	return content, nil
}

// GC removes unreferenced chunks and manifests from local CAS storage
func (s *Store) GC(referencedHashes map[string]bool) (int, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deletedCount := 0
	var freedBytes int64

	// Clean objects/
	objDir := filepath.Join(s.baseDir, "objects")
	_ = filepath.Walk(objDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".chunk") {
			return nil
		}
		hash := strings.TrimSuffix(info.Name(), ".chunk")
		if !referencedHashes[hash] {
			freedBytes += info.Size()
			deletedCount++
			_ = os.Remove(p)
		}
		return nil
	})

	// Clean manifests/
	manDir := filepath.Join(s.baseDir, "manifests")
	_ = filepath.Walk(manDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		hash := strings.TrimSuffix(info.Name(), ".json")
		if !referencedHashes[hash] {
			freedBytes += info.Size()
			deletedCount++
			_ = os.Remove(p)
		}
		return nil
	})

	return deletedCount, freedBytes, nil
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

// SaveManifest writes a BlobManifest into local CAS and syncs to remote if configured
func (s *Store) SaveManifest(m *BlobManifest) error {
	if err := ValidateManifest(m); err != nil {
		return err
	}
	mPath := s.manifestPath(m.BlobHash)
	if err := os.MkdirAll(filepath.Dir(mPath), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(mPath, data, 0644); err != nil {
		return err
	}

	if s.remote != nil {
		if err := s.remote.PutManifest(m.BlobHash, data); err != nil {
			return fmt.Errorf("failed to replicate manifest %s: %w", m.BlobHash, err)
		}
	}
	return nil
}

func (s *Store) saveManifest(m *BlobManifest) error {
	return s.SaveManifest(m)
}

func (s *Store) GetManifest(blobHash string) (*BlobManifest, error) {
	mPath := s.manifestPath(blobHash)
	data, err := os.ReadFile(mPath)
	if err != nil {
		if s.remote != nil {
			s3Data, s3Err := s.remote.GetManifest(blobHash)
			if s3Err == nil {
				_ = os.MkdirAll(filepath.Dir(mPath), 0755)
				_ = os.WriteFile(mPath, s3Data, 0644)
				var m BlobManifest
				if err := json.Unmarshal(s3Data, &m); err == nil {
					return &m, nil
				}
			}
		}
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
