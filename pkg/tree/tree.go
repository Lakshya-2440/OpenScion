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

// EntryType defines whether an entry is a file blob or sub-tree
type EntryType string

const (
	TypeBlob EntryType = "blob"
	TypeTree EntryType = "tree"
)

// Entry represents an item in the Merkle Tree
type Entry struct {
	Path string    `json:"path"` // Relative path from repo root, e.g. "src/main.go"
	Type EntryType `json:"type"`
	Hash string    `json:"hash"` // Blob hash or subtree hash
	Size int64     `json:"size"`
	Mode uint32    `json:"mode"` // 0644 or 0755
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
		return "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // empty sha256
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
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" || pat == "*" || pat == "**" {
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
			if filepath.Dir(path) == dir {
				return true
			}
		} else if strings.HasPrefix(path, pat) {
			return true
		} else if matched, _ := filepath.Match(pat, path); matched {
			return true
		}
	}
	return false
}

// LoadIgnorePatterns loads ignore rules from .gitignore, .scionignore, and default directories
func LoadIgnorePatterns(rootPath string) []string {
	patterns := []string{
		".scion/**", ".scn/**", ".git/**", "mnt/**", "mnt", "node_modules/**", "web/node_modules/**",
		"dist/**", "web/dist/**", "bin/**", "data/**", "*.db", "*.db-wal", "*.db-shm",
		"*.chunk", ".DS_Store", "Thumbs.db", "web/.vercel/**", ".vercel/**",
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

// BuildTreeFromDir scans working dir, puts blobs into CAS store, and returns constructed Tree
func BuildTreeFromDir(rootPath string, store *cas.Store, ignorePatterns []string) (*Tree, error) {
	t := NewTree()
	if len(ignorePatterns) == 0 {
		ignorePatterns = LoadIgnorePatterns(rootPath)
	}

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

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		manifest, err := store.PutBlob(data)
		if err != nil {
			return err
		}

		var mode uint32 = 0644
		if info.Mode()&0111 != 0 {
			mode = 0755
		}

		t.Entries[normalizedRel] = Entry{
			Path: normalizedRel,
			Type: TypeBlob,
			Hash: manifest.BlobHash,
			Size: info.Size(),
			Mode: mode,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	t.ComputeHash()
	return t, nil
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
