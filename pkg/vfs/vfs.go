package vfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/tree"
	"golang.org/x/net/webdav"
)

// VirtualFileInfo implements os.FileInfo
type VirtualFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *VirtualFileInfo) Name() string       { return fi.name }
func (fi *VirtualFileInfo) Size() int64        { return fi.size }
func (fi *VirtualFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *VirtualFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *VirtualFileInfo) IsDir() bool        { return fi.isDir }
func (fi *VirtualFileInfo) Sys() any           { return nil }

// MerkleFS implements webdav.FileSystem on top of OpenScion CAS and Merkle Tree
type MerkleFS struct {
	tree    *tree.Tree
	store   *cas.Store
	modTime time.Time

	dirsMu  sync.RWMutex
	dirTree map[string][]string // clean dir path -> list of child names
	files   map[string]tree.Entry

	// stats
	hydratedMu    sync.Mutex
	hydratedFiles map[string]int64
	bytesServed   int64
}

// NewMerkleFS builds in-memory directory index from Merkle tree
func NewMerkleFS(t *tree.Tree, store *cas.Store, commitTime time.Time) *MerkleFS {
	if commitTime.IsZero() {
		commitTime = time.Now()
	}

	fs := &MerkleFS{
		tree:          t,
		store:         store,
		modTime:       commitTime,
		dirTree:       make(map[string][]string),
		files:         make(map[string]tree.Entry),
		hydratedFiles: make(map[string]int64),
	}

	fs.indexTree()
	return fs
}

func cleanPath(p string) string {
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	if p == "/" {
		return ""
	}
	return strings.TrimPrefix(p, "/")
}

func (fs *MerkleFS) indexTree() {
	fs.dirsMu.Lock()
	defer fs.dirsMu.Unlock()

	dirSet := make(map[string]map[string]bool)
	dirSet[""] = make(map[string]bool)

	for p, entry := range fs.tree.Entries {
		rel := cleanPath(p)
		if rel == "" {
			continue
		}
		fs.files[rel] = entry

		// Build parent hierarchy
		parts := strings.Split(rel, "/")
		for i := 0; i < len(parts); i++ {
			var parent string
			if i > 0 {
				parent = strings.Join(parts[:i], "/")
			}
			child := parts[i]

			if dirSet[parent] == nil {
				dirSet[parent] = make(map[string]bool)
			}
			dirSet[parent][child] = true
		}
	}

	// Flatten sets into sorted slices
	for dir, children := range dirSet {
		list := make([]string, 0, len(children))
		for name := range children {
			list = append(list, name)
		}
		sort.Strings(list)
		fs.dirTree[dir] = list
	}
}

func (fs *MerkleFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return os.ErrPermission
}

func (fs *MerkleFS) RemoveAll(ctx context.Context, name string) error {
	return os.ErrPermission
}

func (fs *MerkleFS) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission
}

func (fs *MerkleFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	rel := cleanPath(name)
	if rel == "" {
		return &VirtualFileInfo{
			name:    "/",
			size:    0,
			mode:    os.ModeDir | 0755,
			modTime: fs.modTime,
			isDir:   true,
		}, nil
	}

	fs.dirsMu.RLock()
	_, isDir := fs.dirTree[rel]
	entry, isFile := fs.files[rel]
	fs.dirsMu.RUnlock()

	if isDir {
		return &VirtualFileInfo{
			name:    path.Base(rel),
			size:    0,
			mode:    os.ModeDir | 0755,
			modTime: fs.modTime,
			isDir:   true,
		}, nil
	}

	if isFile {
		mode := os.FileMode(0644)
		if entry.Mode != 0 {
			mode = os.FileMode(entry.Mode)
		}
		return &VirtualFileInfo{
			name:    path.Base(rel),
			size:    entry.Size,
			mode:    mode,
			modTime: fs.modTime,
			isDir:   false,
		}, nil
	}

	return nil, os.ErrNotExist
}

func (fs *MerkleFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	rel := cleanPath(name)

	fs.dirsMu.RLock()
	children, isDir := fs.dirTree[rel]
	entry, isFile := fs.files[rel]
	fs.dirsMu.RUnlock()

	if rel == "" || isDir {
		// Return directory reader
		var fileInfos []os.FileInfo
		for _, child := range children {
			childPath := child
			if rel != "" {
				childPath = rel + "/" + child
			}
			if subChildren, ok := fs.dirTree[childPath]; ok && len(subChildren) > 0 {
				fileInfos = append(fileInfos, &VirtualFileInfo{
					name:    child,
					size:    0,
					mode:    os.ModeDir | 0755,
					modTime: fs.modTime,
					isDir:   true,
				})
			} else if fe, ok := fs.files[childPath]; ok {
				fileInfos = append(fileInfos, &VirtualFileInfo{
					name:    child,
					size:    fe.Size,
					mode:    0644,
					modTime: fs.modTime,
					isDir:   false,
				})
			}
		}

		baseName := "/"
		if rel != "" {
			baseName = path.Base(rel)
		}

		return &VirtualDirHandle{
			info: &VirtualFileInfo{
				name:    baseName,
				size:    0,
				mode:    os.ModeDir | 0755,
				modTime: fs.modTime,
				isDir:   true,
			},
			entries: fileInfos,
		}, nil
	}

	if isFile {
		// Lazy-hydrate file from CAS chunks on-demand
		start := time.Now()
		content, err := fs.store.GetBlobByHash(entry.Hash)
		if err != nil {
			return nil, fmt.Errorf("failed to hydrate blob %s: %w", entry.Hash, err)
		}
		elapsed := time.Since(start)

		fs.hydratedMu.Lock()
		if _, already := fs.hydratedFiles[rel]; !already {
			fs.hydratedFiles[rel] = int64(len(content))
			fs.bytesServed += int64(len(content))
			fmt.Printf("\033[36m[VFS Lazy-Hydrate]\033[0m %s (%d bytes) in %s\n", rel, len(content), elapsed.Round(time.Microsecond))
		}
		fs.hydratedMu.Unlock()

		return &VirtualFileHandle{
			info: &VirtualFileInfo{
				name:    path.Base(rel),
				size:    int64(len(content)),
				mode:    0644,
				modTime: fs.modTime,
				isDir:   false,
			},
			reader: bytes.NewReader(content),
		}, nil
	}

	return nil, os.ErrNotExist
}

// Stats returns count of hydrated files and bytes
func (fs *MerkleFS) Stats() (int, int64) {
	fs.hydratedMu.Lock()
	defer fs.hydratedMu.Unlock()
	return len(fs.hydratedFiles), fs.bytesServed
}

// VirtualDirHandle implements webdav.File for directories
type VirtualDirHandle struct {
	info    *VirtualFileInfo
	entries []os.FileInfo
	pos     int
}

func (d *VirtualDirHandle) Close() error { return nil }
func (d *VirtualDirHandle) Read(p []byte) (int, error) {
	return 0, io.EOF
}
func (d *VirtualDirHandle) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}
func (d *VirtualDirHandle) Readdir(count int) ([]os.FileInfo, error) {
	if d.pos >= len(d.entries) {
		if count <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}

	if count <= 0 {
		res := d.entries[d.pos:]
		d.pos = len(d.entries)
		return res, nil
	}

	end := d.pos + count
	if end > len(d.entries) {
		end = len(d.entries)
	}
	res := d.entries[d.pos:end]
	d.pos = end
	return res, nil
}
func (d *VirtualDirHandle) Stat() (os.FileInfo, error) {
	return d.info, nil
}
func (d *VirtualDirHandle) Write(p []byte) (int, error) {
	return 0, os.ErrPermission
}

// VirtualFileHandle implements webdav.File for read-only hydrated files
type VirtualFileHandle struct {
	info   *VirtualFileInfo
	reader *bytes.Reader
}

func (f *VirtualFileHandle) Close() error { return nil }
func (f *VirtualFileHandle) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}
func (f *VirtualFileHandle) Seek(offset int64, whence int) (int64, error) {
	return f.reader.Seek(offset, whence)
}
func (f *VirtualFileHandle) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}
func (f *VirtualFileHandle) Stat() (os.FileInfo, error) {
	return f.info, nil
}
func (f *VirtualFileHandle) Write(p []byte) (int, error) {
	return 0, os.ErrPermission
}

// MountOptions configures the VFS mount
type MountOptions struct {
	MountPoint string
	CommitSeq  int64
	ReadOnly   bool
}

// Mount starts the virtual filesystem server and mounts it locally
func Mount(t *tree.Tree, store *cas.Store, commitTime time.Time, opts MountOptions) error {
	fs := NewMerkleFS(t, store, commitTime)

	// Bind to an open ephemeral port on 127.0.0.1
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to bind VFS server port: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	webdavURL := fmt.Sprintf("http://127.0.0.1:%d/", port)

	handler := &webdav.Handler{
		FileSystem: fs,
		LockSystem: webdav.NewMemLS(),
	}

	server := &http.Server{
		Handler: handler,
	}

	mountPoint := opts.MountPoint
	if mountPoint == "" {
		mountPoint = "./mnt"
	}

	absMountPoint, err := filepath.Abs(mountPoint)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(absMountPoint, 0755); err != nil {
		return fmt.Errorf("failed to create mountpoint directory %s: %w", absMountPoint, err)
	}

	// Run WebDAV server in background goroutine
	serverErrChan := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			serverErrChan <- err
		}
	}()

	fmt.Println("\033[1m\033[32mOpenScion Virtual Filesystem (VFS)\033[0m")
	fmt.Printf("• WebDAV Backend:   \033[36m%s\033[0m\n", webdavURL)
	fmt.Printf("• Virtual Mount:    \033[36m%s\033[0m\n", absMountPoint)
	fmt.Printf("• Total Manifest:   %d files\n", len(t.Entries))
	fmt.Println("• Memory Footprint: \033[32m0 bytes on disk\033[0m (lazy-hydrated on read)")
	fmt.Println("------------------------------------------------------------")

	// Attempt OS-specific mount
	mounted := false
	if runtime.GOOS == "darwin" {
		// Use macOS mount_webdav -S (suppress UI dialogs and run non-interactively)
		cmd := exec.Command("/sbin/mount_webdav", "-S", webdavURL, absMountPoint)
		if err := cmd.Run(); err == nil {
			mounted = true
			fmt.Printf("\033[32m✓ Mounted natively to %s via macOS WebDAV VFS\033[0m\n", absMountPoint)
		} else {
			// Try fallback without -S
			cmd2 := exec.Command("/sbin/mount_webdav", webdavURL, absMountPoint)
			if err2 := cmd2.Run(); err2 == nil {
				mounted = true
				fmt.Printf("\033[32m✓ Mounted natively to %s\033[0m\n", absMountPoint)
			}
		}
	} else if runtime.GOOS == "linux" {
		// Linux mount -t davfs if available
		cmd := exec.Command("mount", "-t", "davfs", webdavURL, absMountPoint)
		if err := cmd.Run(); err == nil {
			mounted = true
			fmt.Printf("\033[32m✓ Mounted natively to %s\033[0m\n", absMountPoint)
		}
	} else if runtime.GOOS == "windows" {
		// Windows native WebDAV drive mapping
		cmd := exec.Command("net", "use", "*", webdavURL)
		if out, err := cmd.Output(); err == nil {
			mounted = true
			fmt.Printf("\033[32m✓ Mounted natively via Windows WebDAV network drive: %s\033[0m\n", strings.TrimSpace(string(out)))
		}
	}

	if !mounted {
		fmt.Printf("\033[33mDirect OS kernel mount skipped or requires permissions.\033[0m\n")
		fmt.Printf("Access directly via WebDAV URL: \033[36m%s\033[0m\n", webdavURL)
		if runtime.GOOS == "windows" {
			fmt.Printf("Or in File Explorer: Map Network Drive -> %s\n", webdavURL)
		} else {
			fmt.Printf("Or connect in Finder: Go -> Connect to Server -> %s\n", webdavURL)
		}
	}

	fmt.Println("\n\033[2mStreaming filesystem active. Press Ctrl+C to unmount...\033[0m\n")

	// Setup signal handler for graceful unmount
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		fmt.Println("\nUnmounting OpenScion VFS...")
	case err := <-serverErrChan:
		fmt.Printf("VFS Server error: %v\n", err)
	}

	// Clean unmount
	if mounted {
		if runtime.GOOS == "darwin" {
			_ = exec.Command("diskutil", "unmount", "force", absMountPoint).Run()
			_ = exec.Command("umount", absMountPoint).Run()
		} else if runtime.GOOS == "linux" {
			_ = exec.Command("umount", absMountPoint).Run()
		} else if runtime.GOOS == "windows" {
			_ = exec.Command("net", "use", "*", "/delete", "/y").Run()
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)

	hydrated, bytesCount := fs.Stats()
	fmt.Printf("\033[32m✓ Successfully unmounted.\033[0m (Hydrated %d files, %d bytes total during session)\n", hydrated, bytesCount)
	return nil
}
