package s3store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

const marker = ".ws-folder"
const folderTrash = ".ws-folder-trash/"

func (c *Content) Folders(id string) ([]string, error) {
	if c.isLocal(id) {
		return c.local.Folders(id)
	}
	root, e := c.root(id)
	if e != nil {
		return nil, e
	}
	objects, e := c.objects(root)
	if e != nil {
		return nil, e
	}
	found := map[string]bool{}
	for _, o := range objects {
		rel := strings.TrimPrefix(aws.ToString(o.Key), root)
		parts := strings.Split(rel, "/")
		for i := 0; i < len(parts)-1; i++ {
			if strings.HasPrefix(parts[i], ".") {
				break
			}
			found[strings.Join(parts[:i+1], "/")] = true
		}
	}
	out := []string{}
	for p := range found {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
func (c *Content) MakeFolder(id, path string) error {
	if c.isLocal(id) {
		return c.local.MakeFolder(id, path)
	}
	k, e := c.key(id, path)
	if e != nil {
		return e
	}
	if hidden(path) {
		return domain.Invalid("reserved folder")
	}
	return c.put(k+"/"+marker, nil)
}
func (c *Content) RemoveFolder(id, path string) error {
	if c.isLocal(id) {
		return c.local.RemoveFolder(id, path)
	}
	k, e := c.key(id, path)
	if e != nil {
		return e
	}
	objects, e := c.objects(k + "/")
	if e != nil {
		return e
	}
	for _, o := range objects {
		if !strings.HasSuffix(aws.ToString(o.Key), "/"+marker) {
			return domain.Conflict("folder is not empty")
		}
	}
	for _, o := range objects {
		if e = c.remove(aws.ToString(o.Key)); e != nil {
			return e
		}
	}
	return nil
}

// relocate first copies every object, then removes the sources. Failure rolls
// back from intact copies; a failed rollback is surfaced for journal recovery.
func (c *Content) relocate(from, to string) error {
	objects, e := c.objects(from)
	if e != nil {
		return e
	}
	copied := []string{}
	for _, o := range objects {
		src := aws.ToString(o.Key)
		dst := to + strings.TrimPrefix(src, from)
		if e = c.copy(src, dst); e != nil {
			for _, key := range copied {
				_ = c.remove(key)
			}
			return e
		}
		copied = append(copied, dst)
	}
	for _, o := range objects {
		if e = c.remove(aws.ToString(o.Key)); e != nil {
			var rollback error
			for i, original := range objects {
				if err := c.copy(copied[i], aws.ToString(original.Key)); err != nil {
					rollback = errors.Join(rollback, err)
				}
			}
			if rollback == nil {
				for _, key := range copied {
					_ = c.remove(key)
				}
			}
			return errors.Join(e, rollback)
		}
	}
	return nil
}
func (c *Content) MoveFolder(id, from, to string) error {
	if c.isLocal(id) {
		return c.local.MoveFolder(id, from, to)
	}
	src, e := c.key(id, from)
	if e != nil {
		return e
	}
	dst, e := c.key(id, to)
	if e != nil {
		return e
	}
	if src == dst || strings.HasPrefix(dst, src+"/") || strings.HasPrefix(src, dst+"/") {
		return domain.Invalid("overlapping folder paths")
	}
	objects, e := c.objects(dst + "/")
	if e != nil {
		return e
	}
	if len(objects) > 0 {
		return domain.Conflict("destination folder exists")
	}
	exists, e := c.exists(dst)
	if e != nil {
		return e
	}
	if exists {
		return domain.Conflict("destination path exists")
	}
	objects, e = c.objects(src + "/")
	if e != nil {
		return e
	}
	if len(objects) == 0 {
		return fs.ErrNotExist
	}
	return c.relocate(src+"/", dst+"/")
}
func (c *Content) StashFolder(id, path, trashID string, docIDs []string) error {
	if c.isLocal(id) {
		return c.local.StashFolder(id, path, trashID, docIDs)
	}
	if !validTrashID(trashID) {
		return domain.Invalid("invalid trash id")
	}
	root, e := c.root(id)
	if e != nil {
		return e
	}
	src, e := c.key(id, path)
	if e != nil {
		return e
	}
	entry := ports.FolderTrashEntry{TrashID: trashID, Path: path, DocIDs: docIDs, TrashedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	b, e := json.Marshal(entry)
	if e != nil {
		return e
	}
	base := root + folderTrash + trashID + "/"
	if e = c.put(base+"entry.json", b); e != nil {
		return e
	}
	if e = c.relocate(src+"/", base+"content/"); e != nil {
		_ = c.remove(base + "entry.json")
		return e
	}
	return nil
}
func validTrashID(id string) bool {
	return strings.HasPrefix(id, "ftr_") && !strings.ContainsAny(id, "/.\\")
}
func (c *Content) StashedFolders(id string) ([]ports.FolderTrashEntry, error) {
	if c.isLocal(id) {
		return c.local.StashedFolders(id)
	}
	root, e := c.root(id)
	if e != nil {
		return nil, e
	}
	objects, e := c.objects(root + folderTrash)
	if e != nil {
		return nil, e
	}
	out := []ports.FolderTrashEntry{}
	for _, o := range objects {
		if !strings.HasSuffix(aws.ToString(o.Key), "/entry.json") {
			continue
		}
		b, e := c.get(aws.ToString(o.Key))
		if e != nil {
			return nil, e
		}
		var entry ports.FolderTrashEntry
		if e = json.Unmarshal(b, &entry); e != nil {
			return nil, e
		}
		if entry.Kind == "attachment" {
			exists, err := c.exists(root + folderTrash + entry.TrashID + "/content/file")
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TrashedAt > out[j].TrashedAt })
	return out, nil
}
func (c *Content) UnstashFolder(id, trashID string) (string, []string, error) {
	if c.isLocal(id) {
		return c.local.UnstashFolder(id, trashID)
	}
	if !validTrashID(trashID) {
		return "", nil, domain.Invalid("invalid trash id")
	}
	root, e := c.root(id)
	if e != nil {
		return "", nil, e
	}
	base := root + folderTrash + trashID + "/"
	b, e := c.get(base + "entry.json")
	if e != nil {
		return "", nil, e
	}
	var entry ports.FolderTrashEntry
	if e = json.Unmarshal(b, &entry); e != nil {
		return "", nil, e
	}
	if entry.Kind == "attachment" {
		return c.unstashAttachment(id, base, entry.Path)
	}
	dest, e := c.key(id, entry.Path)
	if e != nil {
		return "", nil, e
	}
	objects, e := c.objects(base + "content/")
	if e != nil {
		return "", nil, e
	}
	conflicts := []string{}
	for _, o := range objects {
		src := aws.ToString(o.Key)
		rel := strings.TrimPrefix(src, base+"content/")
		dst := dest + "/" + rel
		exists, e := c.exists(dst)
		if e != nil {
			return entry.Path, conflicts, e
		}
		if exists && !strings.HasSuffix(dst, "/"+marker) {
			conflicts = append(conflicts, entry.Path+"/"+rel)
			continue
		}
		if e = c.copy(src, dst); e != nil {
			return entry.Path, conflicts, e
		}
		if e = c.remove(src); e != nil {
			return entry.Path, conflicts, e
		}
	}
	if len(conflicts) == 0 {
		if e = c.remove(base + "entry.json"); e != nil {
			return entry.Path, conflicts, e
		}
	}
	return entry.Path, conflicts, nil
}

var _ ports.FolderStore = (*Content)(nil)
