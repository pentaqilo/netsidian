// Package local implements the managed local content adapter: exclusive
// space subdirectories under an operator root, atomic replace writes and
// protected journal/history state outside served content roots.
package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

type Content struct {
	Root     string // operator-defined managed root
	mu       sync.RWMutex
	external map[string]string
}

func NewContent(root string) (*Content, error) {
	if root == "" {
		return nil, errors.New("local content root required")
	}
	if err := os.MkdirAll(filepath.Join(root, "spaces"), 0o700); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Content{Root: abs, external: make(map[string]string)}, nil
}

// BindExternal maps one native vault to an existing directory. The binding is
// explicit and scoped to the opaque space ID; it never changes other vaults.
func (c *Content) BindExternal(spaceID, root string) error {
	if !ValidSpaceID(spaceID) || !filepath.IsAbs(root) {
		return domain.Invalid("invalid local vault binding")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return domain.Invalid("local vault root must be a real directory")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.external[spaceID] = abs
	return nil
}

func (c *Content) UnbindExternal(spaceID string) {
	c.mu.Lock()
	delete(c.external, spaceID)
	c.mu.Unlock()
}

func (c *Content) spaceRoot(spaceID string) (string, error) {
	if !ValidSpaceID(spaceID) {
		return "", domain.Invalid("space id invalid")
	}
	c.mu.RLock()
	root, external := c.external[spaceID]
	c.mu.RUnlock()
	if external {
		return root, nil
	}
	return filepath.Join(c.Root, "spaces", spaceID), nil
}

func (c *Content) PurgeSpace(spaceID string) error {
	if !ValidSpaceID(spaceID) {
		return domain.Invalid("space id invalid")
	}
	c.mu.RLock()
	_, external := c.external[spaceID]
	c.mu.RUnlock()
	if external {
		c.UnbindExternal(spaceID)
		return nil
	} // unlink; never delete a chosen folder
	return os.RemoveAll(filepath.Join(c.Root, "spaces", spaceID))
}

// ValidSpaceID accepts only opaque generated ids; nothing that could
// traverse (no separators, no leading dot) ever reaches Join.
func ValidSpaceID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-':
		case i == 0:
			return false // must not start with separator or dot
		default:
			return false
		}
	}
	return true
}

// resolve validates containment after joining.
func (c *Content) resolve(spaceID, relPath string) (string, error) {
	if !ValidSpaceID(spaceID) {
		return "", domain.Invalid("space id invalid")
	}
	_, _, perr := domain.NormalizePath(relPath)
	if perr != nil {
		return "", perr
	}
	base, err := c.spaceRoot(spaceID)
	if err != nil {
		return "", err
	}
	full := filepath.Join(base, filepath.FromSlash(relPath))
	// defense in depth: final path must remain under the space root
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", domain.Invalid("path escapes space root")
	}
	for check := full; ; check = filepath.Dir(check) {
		info, statErr := os.Lstat(check)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", domain.Invalid("symlinks are not supported in managed storage")
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", statErr
		}
		if check == base || check == filepath.Dir(check) {
			break
		}
	}
	return full, nil
}

func (c *Content) WriteAtomic(spaceID, relPath string, data []byte) error {
	full, err := c.resolve(spaceID, relPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".ws-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // flush before replace
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, full); err != nil {
		return err
	}
	cleanup = false
	// flush directory metadata where supported
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

func (c *Content) Read(spaceID, relPath string) ([]byte, error) {
	full, err := c.resolve(spaceID, relPath)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return nil, domain.NotFound("content bytes missing")
	}
	return b, err
}

func (c *Content) Exists(spaceID, relPath string) (bool, error) {
	full, err := c.resolve(spaceID, relPath)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(full)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *Content) Move(spaceID, fromRel, toRel string) error {
	from, err := c.resolve(spaceID, fromRel)
	if err != nil {
		return err
	}
	to, err := c.resolve(spaceID, toRel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(to)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

func (c *Content) Delete(spaceID, relPath string) error {
	full, err := c.resolve(spaceID, relPath)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ---------- State store (journal + immutable history) ----------

type State struct {
	JournalDir  string
	HistoryRoot string
}

func (s *State) PurgeSpace(spaceID string) error {
	if !ValidSpaceID(spaceID) {
		return domain.Invalid("space id invalid")
	}
	return os.RemoveAll(filepath.Join(s.HistoryRoot, spaceID))
}

func NewState(journalDir, historyRoot string) (*State, error) {
	for _, d := range []string{journalDir, historyRoot} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	return &State{JournalDir: journalDir, HistoryRoot: historyRoot}, nil
}

func (s *State) WriteJournal(opID string, data []byte) (string, error) {
	if strings.ContainsAny(opID, `/\`) {
		return "", domain.Invalid("op id invalid")
	}
	full := filepath.Join(s.JournalDir, opID+".pending")
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	f, err := os.Open(tmp)
	if err == nil {
		_ = f.Sync()
		f.Close()
	}
	if err := os.Rename(tmp, full); err != nil {
		return "", err
	}
	return full, nil
}

// SaveHistory stores the immutable bytes for one committed revision.
func (s *State) SaveHistory(spaceID, docID, revID string, data []byte) (string, error) {
	dir := filepath.Join(s.HistoryRoot, spaceID, docID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	full := filepath.Join(dir, revID+".md")
	if _, err := os.Stat(full); err == nil {
		return full, nil // already durable
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	f, err := os.Open(tmp)
	if err == nil {
		_ = f.Sync()
		f.Close()
	}
	if err := os.Rename(tmp, full); err != nil {
		return "", err
	}
	return full, nil
}

func (s *State) ReadAbsolute(path string) ([]byte, error) {
	if !strings.HasPrefix(path, s.JournalDir) && !strings.HasPrefix(path, s.HistoryRoot) {
		return nil, fmt.Errorf("state path outside protected roots")
	}
	return os.ReadFile(path)
}

func (s *State) RemoveAbsolute(path string) error {
	if !strings.HasPrefix(path, s.JournalDir) && !strings.HasPrefix(path, s.HistoryRoot) {
		return fmt.Errorf("state path outside protected roots")
	}
	return os.Remove(path)
}

func (s *State) HistoryExists(location string) (bool, error) {
	if !strings.HasPrefix(location, s.HistoryRoot) {
		return false, fmt.Errorf("state path outside protected roots")
	}
	_, err := os.Stat(location)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (c *Content) Folders(spaceID string) ([]string, error) {
	if !ValidSpaceID(spaceID) {
		return nil, domain.Invalid("invalid space")
	}
	base, err := c.spaceRoot(spaceID)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	_, external := c.external[spaceID]
	c.mu.RUnlock()
	out := []string{}
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == base {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if external {
				return nil
			} // visible vaults may contain links; never follow them
			return domain.Invalid("symlinks are not supported in managed storage")
		}
		if !d.IsDir() || path == base {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || (external && d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}
func (c *Content) MakeFolder(spaceID, path string) error {
	full, err := c.resolve(spaceID, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(full, 0700); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(full))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ---------- folder rename / move / empty removal ----------

func (c *Content) MoveFolder(spaceID, fromRel, toRel string) error {
	from, err := c.resolve(spaceID, fromRel)
	if err != nil {
		return err
	}
	to, err := c.resolve(spaceID, toRel)
	if err != nil {
		return err
	}
	if from == to {
		return domain.Invalid("source and target folder are identical")
	}
	if _, statErr := os.Lstat(to); statErr == nil {
		return domain.Conflict("target folder already exists")
	}
	info, err := os.Lstat(from)
	if err != nil || !info.IsDir() {
		return domain.NotFound("folder not found")
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(from), filepath.Dir(to)} {
		if d, err := os.Open(dir); err == nil {
			_ = d.Sync()
			d.Close()
		}
	}
	return nil
}

func (c *Content) RemoveFolder(spaceID, path string) error {
	full, err := c.resolve(spaceID, path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(full)
	if err != nil || !info.IsDir() {
		return nil // nothing to clean
	}
	if entries, err := os.ReadDir(full); err == nil && len(entries) > 0 {
		return domain.Conflict("folder is not empty")
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	if d, err := os.Open(filepath.Dir(full)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// ---------- folder trash area ----------

// folderTrashRoot returns the protected folder-trash directory of a space.
func (c *Content) folderTrashRoot(spaceID string) (string, error) {
	if !ValidSpaceID(spaceID) {
		return "", domain.Invalid("invalid space")
	}
	base, err := c.spaceRoot(spaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ".netsidian-trash", "folder"), nil
}

func validTrashID(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		case i == 0:
			return false
		default:
			return false
		}
	}
	return true
}

type folderTrashMeta struct {
	Kind      string   `json:"kind,omitempty"`
	Path      string   `json:"path"`
	TrashedAt string   `json:"trashedAt"`
	DocIDs    []string `json:"docIds"`
}

func (c *Content) StashFolder(spaceID, path, trashID string, docIDs []string) error {
	if !validTrashID(trashID) {
		return domain.Invalid("trash id invalid")
	}
	src, err := c.resolve(spaceID, path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil || !info.IsDir() {
		return domain.NotFound("folder not found")
	}
	root, err := c.folderTrashRoot(spaceID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	dst := filepath.Join(root, trashID)
	if _, statErr := os.Lstat(dst); statErr == nil {
		return domain.Conflict("trash entry already exists")
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	meta := folderTrashMeta{Path: path, TrashedAt: time.Now().UTC().Format(time.RFC3339), DocIDs: docIDs}
	b, _ := json.Marshal(meta)
	return os.WriteFile(filepath.Join(root, trashID+".json"), b, 0o600)
}

func (c *Content) StashedFolders(spaceID string) ([]ports.FolderTrashEntry, error) {
	root, err := c.folderTrashRoot(spaceID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []ports.FolderTrashEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []ports.FolderTrashEntry{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		var meta folderTrashMeta
		if err := json.Unmarshal(b, &meta); err != nil || meta.Path == "" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if meta.Kind == "attachment" {
			_, content := attachmentTrashPaths(id)
			exists, err := c.Exists(spaceID, content)
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}
		}
		out = append(out, ports.FolderTrashEntry{TrashID: id, Kind: meta.Kind, Path: meta.Path, TrashedAt: meta.TrashedAt, DocIDs: meta.DocIDs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TrashedAt > out[j].TrashedAt })
	return out, nil
}

func (c *Content) UnstashFolder(spaceID, trashID string) (string, []string, error) {
	if !validTrashID(trashID) {
		return "", nil, domain.Invalid("trash id invalid")
	}
	root, err := c.folderTrashRoot(spaceID)
	if err != nil {
		return "", nil, err
	}
	mb, err := os.ReadFile(filepath.Join(root, trashID+".json"))
	if err != nil {
		return "", nil, domain.NotFound("trash entry not found")
	}
	var meta folderTrashMeta
	if err := json.Unmarshal(mb, &meta); err != nil || meta.Path == "" {
		return "", nil, domain.NotFound("trash entry not found")
	}
	if meta.Kind == "attachment" {
		return c.unstashAttachment(spaceID, trashID, meta.Path)
	}
	dst, err := c.resolve(spaceID, meta.Path)
	if err != nil {
		return "", nil, err
	}
	src := filepath.Join(root, trashID)
	if _, err := os.Lstat(src); err != nil {
		return "", nil, domain.NotFound("trashed folder bytes missing")
	}
	conflicts, err := mergeTree(src, dst)
	if err != nil {
		return "", conflicts, err
	}
	if len(conflicts) > 0 {
		// conflicting originals keep their recovery record and bytes;
		// neither version is destroyed and the merge can be retried.
		return meta.Path, conflicts, nil
	}
	_ = os.RemoveAll(src)
	_ = os.Remove(filepath.Join(root, trashID+".json"))
	return meta.Path, nil, nil
}

// mergeTree moves every file from src into dst, creating parents on the
// way. Existing destination files are never overwritten: the stashed file
// stays in place and its space-relative path is reported as a conflict.
func mergeTree(src, dst string) ([]string, error) {
	var files []string
	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var conflicts []string
	for _, path := range files {
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return conflicts, rerr
		}
		target := filepath.Join(dst, rel)
		if _, statErr := os.Lstat(target); statErr == nil {
			conflicts = append(conflicts, filepath.ToSlash(rel))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return conflicts, err
		}
		if err := os.Rename(path, target); err != nil {
			return conflicts, err
		}
	}
	return conflicts, nil
}

// ---------- file listing (attachments) ----------

func (c *Content) ListFiles(spaceID, prefix string) ([]ports.FileEntry, error) {
	c.mu.RLock()
	_, external := c.external[spaceID]
	c.mu.RUnlock()
	var base string
	var err error
	if prefix == "" || prefix == "." {
		if !ValidSpaceID(spaceID) {
			return nil, domain.Invalid("space id invalid")
		}
		base, err = c.spaceRoot(spaceID)
		if err != nil {
			return nil, err
		}
	} else {
		base, err = c.resolve(spaceID, prefix)
		if err != nil {
			return nil, err
		}
	}
	root, err := c.spaceRoot(spaceID)
	if err != nil {
		return nil, err
	}
	out := []ports.FileEntry{}
	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if path != base && (strings.HasPrefix(d.Name(), ".") || (external && d.Name() == "node_modules")) {
				return fs.SkipDir // protected areas (.netsidian, .netsidian-trash)
			}
			return nil
		}
		if strings.HasPrefix(rel, "..") {
			return fs.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		for _, seg := range strings.Split(rel, "/") {
			if strings.HasPrefix(seg, ".") {
				return nil
			}
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		out = append(out, ports.FileEntry{Path: rel, Size: info.Size()})
		return nil
	})
	if os.IsNotExist(walkErr) {
		return out, nil
	}
	return out, walkErr
}
