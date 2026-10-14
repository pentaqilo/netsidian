package local

import (
	"encoding/json"
	"os"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func attachmentTrashPaths(id string) (meta, content string) {
	return ".netsidian-trash/folder/" + id + ".json", ".netsidian-trash/folder/" + id + "/content"
}
func (c *Content) StashAttachment(space, path, id string) error {
	if !validTrashID(id) {
		return domain.Invalid("invalid trash id")
	}
	c.mu.RLock()
	_, external := c.external[space]
	c.mu.RUnlock()
	if external {
		return domain.Unsupported("attached folders do not support app trash")
	}
	src, err := c.resolve(space, path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil || !info.Mode().IsRegular() {
		return domain.NotFound("attachment not found")
	}
	meta, target := attachmentTrashPaths(id)
	if exists, err := c.Exists(space, meta); err != nil {
		return err
	} else if exists {
		return domain.Conflict("trash entry already exists")
	}
	data, err := json.Marshal(folderTrashMeta{Path: path, Kind: "attachment", TrashedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	// Record first: interruption after the rename always leaves recoverable metadata.
	if err := c.WriteAtomic(space, meta, data); err != nil {
		return err
	}
	if err := c.Move(space, path, target); err != nil {
		_ = c.Delete(space, meta)
		return err
	}
	return nil
}
func (c *Content) unstashAttachment(space, id, path string) (string, []string, error) {
	c.mu.RLock()
	_, external := c.external[space]
	c.mu.RUnlock()
	if external {
		return "", nil, domain.Unsupported("attached folders do not support app trash")
	}
	meta, src := attachmentTrashPaths(id)
	if exists, err := c.Exists(space, path); err != nil {
		return "", nil, err
	} else if exists {
		return path, []string{path}, nil
	}
	if exists, err := c.Exists(space, src); err != nil {
		return "", nil, err
	} else if !exists {
		return "", nil, domain.NotFound("trashed attachment bytes missing")
	}
	if err := c.Move(space, src, path); err != nil {
		return "", nil, err
	}
	if err := c.Delete(space, meta); err != nil {
		return path, nil, err
	}
	_ = c.RemoveFolder(space, ".netsidian-trash/folder/"+id)
	return path, nil, nil
}
