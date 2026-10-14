package s3store

import (
	"encoding/json"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"time"
)

func (c *Content) StashAttachment(space, path, id string) error {
	if c.isLocal(space) {
		return c.local.StashAttachment(space, path, id)
	}
	if !validTrashID(id) {
		return domain.Invalid("invalid trash id")
	}
	root, err := c.root(space)
	if err != nil {
		return err
	}
	src, err := c.key(space, path)
	if err != nil {
		return err
	}
	exists, err := c.exists(src)
	if err != nil {
		return err
	}
	if !exists {
		return domain.NotFound("attachment not found")
	}
	base := root + folderTrash + id + "/"
	if exists, err := c.exists(base + "entry.json"); err != nil {
		return err
	} else if exists {
		return domain.Conflict("trash entry already exists")
	}
	entry := ports.FolderTrashEntry{TrashID: id, Kind: "attachment", Path: path, TrashedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := c.put(base+"entry.json", data); err != nil {
		return err
	}
	if err := c.copy(src, base+"content/file"); err != nil {
		_ = c.remove(base + "entry.json")
		return err
	}
	// Keep both the record and copied bytes if deleting the source fails.
	return c.remove(src)
}
func (c *Content) unstashAttachment(space, base, path string) (string, []string, error) {
	dst, err := c.key(space, path)
	if err != nil {
		return "", nil, err
	}
	if exists, err := c.exists(dst); err != nil {
		return "", nil, err
	} else if exists {
		return path, []string{path}, nil
	}
	src := base + "content/file"
	if exists, err := c.exists(src); err != nil {
		return "", nil, err
	} else if !exists {
		return "", nil, domain.NotFound("trashed attachment bytes missing")
	}
	if err := c.copy(src, dst); err != nil {
		return "", nil, err
	}
	if err := c.remove(src); err != nil {
		return path, nil, err
	}
	if err := c.remove(base + "entry.json"); err != nil {
		return path, nil, err
	}
	return path, nil, nil
}
