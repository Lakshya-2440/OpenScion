package tree

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aether/scn/pkg/cas"
)

func (t *Tree) Validate() error {
	if t == nil {
		return fmt.Errorf("tree is nil")
	}
	expected := t.RootHash
	for path, entry := range t.Entries {
		clean := filepath.ToSlash(filepath.Clean(path))
		if clean == "." || clean == "" || filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("tree contains unsafe path %q", path)
		}
		if entry.Path != path {
			return fmt.Errorf("tree entry path mismatch for %q", path)
		}
		if entry.Type != TypeBlob || entry.Hash == "" {
			return fmt.Errorf("tree entry %q is not a valid blob", path)
		}
	}
	if t.ComputeHash() != expected {
		return fmt.Errorf("tree root hash mismatch")
	}
	return nil
}

// EntryType defines whether an entry is a file blob or sub-tree
type EntryType string

const (
	TypeBlob EntryType = "blob"
	TypeTree EntryType = "tree"
)

// Entry represents an item in the Merkle Tree
type Entry struct {
	Path    string    `json:"path"` // Relative path from repo root, e.g. "src/main.go"
	Type    EntryType `json:"type"`
	Hash    string    `json:"hash"` // Blob hash or subtree hash
	Size    int64     `json:"size"`
	Mode    uint32    `json:"mode"` // 0644 or 0755
	ModTime int64     `json:"mod_time,omitempty"`
}

// Tree contains all entries for a snapshot
type Tree struct {
	RootHash string           `json:"root_hash"`
	Entries  map[string]Entry `json:"entries"` // Key is relative path
}

// NewTree creates an empty Tree
func NewTree() *Tree {
	return &Tree{
		Entries: make(map[string]Entry),
	}
}

// ComputeHash calculates deterministic Merkle root hash for all entries in the tree
func (t *Tree) ComputeHash() string {
	if len(t.Entries) == 0 {
		t.RootHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // empty sha256
		return t.RootHash
	}

	keys := make([]string, 0, len(t.Entries))
	for k := range t.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		e := t.Entries[k]
		fmt.Fprintf(h, "%d:%s:%s:%s:%d\n", e.Mode, e.Type, e.Path, e.Hash, e.Size)
	}
	t.RootHash = hex.EncodeToString(h.Sum(nil))
	return t.RootHash
}

// ChangeType represents diff status
type ChangeType string

const (
	ChangeAdded    ChangeType = "A"
	ChangeModified ChangeType = "M"
	ChangeDeleted  ChangeType = "D"
)

type DiffEntry struct {
	Path       string     `json:"path"`
	ChangeType ChangeType `json:"change_type"`
	OldHash    string     `json:"old_hash,omitempty"`
	NewHash    string     `json:"new_hash,omitempty"`
	OldSize    int64      `json:"old_size,omitempty"`
	NewSize    int64      `json:"new_size,omitempty"`
}

// Diff compares baseTree to targetTree and produces file-level diff entries
func Diff(baseTree, targetTree *Tree) []DiffEntry {
	var diffs []DiffEntry

	baseEntries := make(map[string]Entry)
	if baseTree != nil {
		baseEntries = baseTree.Entries
	}

	targetEntries := make(map[string]Entry)
	if targetTree != nil {
		targetEntries = targetTree.Entries
	}

	allPaths := make(map[string]bool)
	for p := range baseEntries {
		allPaths[p] = true
	}
	for p := range targetEntries {
		allPaths[p] = true
	}

	sortedPaths := make([]string, 0, len(allPaths))
	for p := range allPaths {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	for _, p := range sortedPaths {
		oldEntry, hadOld := baseEntries[p]
		newEntry, hadNew := targetEntries[p]

		if !hadOld && hadNew {
			diffs = append(diffs, DiffEntry{
				Path:       p,
				ChangeType: ChangeAdded,
				NewHash:    newEntry.Hash,
				NewSize:    newEntry.Size,
			})
		} else if hadOld && !hadNew {
			diffs = append(diffs, DiffEntry{
				Path:       p,
				ChangeType: ChangeDeleted,
				OldHash:    oldEntry.Hash,
				OldSize:    oldEntry.Size,
			})
		} else if hadOld && hadNew && oldEntry.Hash != newEntry.Hash {
			diffs = append(diffs, DiffEntry{
				Path:       p,
				ChangeType: ChangeModified,
				OldHash:    oldEntry.Hash,
				NewHash:    newEntry.Hash,
				OldSize:    oldEntry.Size,
				NewSize:    newEntry.Size,
			})
		}
	}

	return diffs
}

// FilterSparse returns a new Tree with only files matching patterns
func (t *Tree) FilterSparse(patterns []string) *Tree {
	if len(patterns) == 0 {
		return t
	}

	filtered := NewTree()
	for p, entry := range t.Entries {
		if MatchesAnyPattern(p, patterns) {
			filtered.Entries[p] = entry
		}
	}
	filtered.ComputeHash()
	return filtered
}

// MatchesAnyPattern checks if a relative path matches any glob/prefix pattern
func MatchesAnyPattern(path string, patterns []string) bool {
	baseName := filepath.Base(path)
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if pat == "*" || pat == "**" {
			return true
		}
		// Exact match or prefix match with wildcard
		if strings.HasSuffix(pat, "/**") {
			prefix := strings.TrimSuffix(pat, "/**")
			if strings.HasPrefix(path, prefix+"/") || path == prefix {
				return true
			}
		} else if strings.HasSuffix(pat, "/*") {
			dir := strings.TrimSuffix(pat, "/*")
			if filepath.ToSlash(filepath.Dir(path)) == dir {
				return true
			}
		} else if path == pat || strings.HasPrefix(path, pat+"/") {
			return true
		} else if matched, _ := filepath.Match(pat, path); matched {
			return true
		} else if matched, _ := filepath.Match(pat, baseName); matched {
			return true
		}
	}
	return false
}

// LoadIgnorePatterns loads ignore rules from .gitignore, .scionignore, and default directories
func LoadIgnorePatterns(rootPath string) []string {
	patterns := []string{
		".scion/**", ".scn/**", ".git/**", "mnt/**", "mnt", "node_modules/**",
		"dist/**", "bin/**", "data/**", "*.db", "*.db-wal", "*.db-shm",
		"*.chunk", ".DS_Store", "Thumbs.db", "AGENTS.md", "CLAUDE.md",
	}

	for _, name := range []string{".gitignore", ".scionignore"} {
		fpath := filepath.Join(rootPath, name)
		data, err := os.ReadFile(fpath)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				line = strings.TrimPrefix(line, "/")
				if strings.HasSuffix(line, "/") {
					patterns = append(patterns, line+"**")
				} else {
					patterns = append(patterns, line)
					patterns = append(patterns, line+"/**")
				}
			}
		}
	}
	return patterns
}

// BuildTreeFromDirWithBase scans working dir and reuses existing entries from baseTree if size, mode and modtime match
func BuildTreeFromDirWithBase(rootPath string, store *cas.Store, ignorePatterns []string, baseTree *Tree) (*Tree, error) {
	return BuildTreeFromDirWithBaseAndSparse(rootPath, store, ignorePatterns, baseTree, nil)
}

// BuildTreeFromDirWithBaseAndSparse scans working dir, preserving entries outside sparse patterns from baseTree
func BuildTreeFromDirWithBaseAndSparse(rootPath string, store *cas.Store, ignorePatterns []string, baseTree *Tree, sparsePatterns []string) (*Tree, error) {
	t := NewTree()
	if len(ignorePatterns) == 0 {
		ignorePatterns = LoadIgnorePatterns(rootPath)
	}

	caseMap := make(map[string]string)

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info != nil && info.IsDir() {
			name := info.Name()
			if name == "mnt" || name == ".scion" || name == ".scn" || name == ".git" || name == "node_modules" || name == ".vercel" {
				return filepath.SkipDir
			}
		}

		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return nil
		}
		if relPath == "." {
			return nil
		}

		normalizedRel := filepath.ToSlash(relPath)
		if MatchesAnyPattern(normalizedRel, ignorePatterns) || MatchesAnyPattern(info.Name(), ignorePatterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Detect case-folding collisions on case-insensitive filesystems (macOS APFS / Windows NTFS)
		lower := strings.ToLower(normalizedRel)
		if existing, collided := caseMap[lower]; collided && existing != normalizedRel {
			return fmt.Errorf("case collision detected: %q and %q collide on case-insensitive filesystems", existing, normalizedRel)
		}
		caseMap[lower] = normalizedRel

		fileMode := info.Mode()
		// Safely skip sockets, FIFOs (named pipes), and devices that would hang os.ReadFile or error
		if fileMode&(os.ModeSocket|os.ModeNamedPipe|os.ModeDevice|os.ModeCharDevice) != 0 {
			return nil
		}

		isSymlink := fileMode&os.ModeSymlink != 0
		var linkTarget string
		if isSymlink {
			target, err := os.Readlink(path)
			if err != nil {
				return nil // Broken or inaccessible symlink: skip without aborting scan
			}
			linkTarget = target
		} else if !fileMode.IsRegular() {
			return nil
		}

		var mode uint32 = 0644
		if fileMode&0111 != 0 {
			mode = 0755
		}
		if isSymlink {
			mode = 0120000
		}
		modTime := info.ModTime().UnixNano()

		fileSize := info.Size()
		if isSymlink {
			fileSize = int64(len(linkTarget))
		}

		// Cache check: reuse entry if baseTree entry matches size, mode, and modtime
		if baseTree != nil {
			if oldEntry, ok := baseTree.Entries[normalizedRel]; ok {
				if oldEntry.Size == fileSize && oldEntry.Mode == mode {
					if oldEntry.ModTime == 0 || oldEntry.ModTime == modTime {
						t.Entries[normalizedRel] = Entry{
							Path:    normalizedRel,
							Type:    TypeBlob,
							Hash:    oldEntry.Hash,
							Size:    oldEntry.Size,
							Mode:    mode,
							ModTime: modTime,
						}
						return nil
					}
				}
			}
		}

		var manifest *cas.BlobManifest
		if isSymlink {
			m, err := store.PutBlob([]byte(linkTarget))
			if err != nil {
				return nil
			}
			manifest = m
		} else if fileSize > cas.ChunkSize {
			// Stream large files directly in chunks to prevent heap spikes / OOM
			m, err := store.PutBlobFromFile(path)
			if err != nil {
				return nil
			}
			manifest = m
		} else {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			m, err := store.PutBlob(data)
			if err != nil {
				return nil
			}
			manifest = m
		}

		t.Entries[normalizedRel] = Entry{
			Path:    normalizedRel,
			Type:    TypeBlob,
			Hash:    manifest.BlobHash,
			Size:    fileSize,
			Mode:    mode,
			ModTime: modTime,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Preserve entries outside sparse pattern from baseTree to prevent wiping unmanaged monorepo directories
	if baseTree != nil && len(sparsePatterns) > 0 && !MatchesAnyPattern("*", sparsePatterns) && !MatchesAnyPattern("**", sparsePatterns) {
		for basePath, baseEntry := range baseTree.Entries {
			if !MatchesAnyPattern(basePath, sparsePatterns) {
				if _, exists := t.Entries[basePath]; !exists {
					t.Entries[basePath] = baseEntry
				}
			}
		}
	}

	t.ComputeHash()
	return t, nil
}

// BuildTreeFromDir scans working dir, puts blobs into CAS store, and returns constructed Tree
func BuildTreeFromDir(rootPath string, store *cas.Store, ignorePatterns []string) (*Tree, error) {
	return BuildTreeFromDirWithBase(rootPath, store, ignorePatterns, nil)
}

// Serialize converts tree to JSON
func (t *Tree) Serialize() ([]byte, error) {
	return json.Marshal(t)
}

// Deserialize loads tree from JSON
func Deserialize(data []byte) (*Tree, error) {
	var t Tree
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
