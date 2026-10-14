package providers

import (
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func (m *Manager) Folders(id string) ([]string, error) {
	c, e := m.store(id)
	if e != nil {
		return nil, e
	}
	f, ok := c.(ports.FolderStore)
	if !ok {
		return nil, domain.Unsupported("provider does not support folders")
	}
	return f.Folders(id)
}
func (m *Manager) MakeFolder(id, p string) error {
	c, e := m.store(id)
	if e != nil {
		return e
	}
	f, ok := c.(ports.FolderStore)
	if !ok {
		return domain.Unsupported("provider does not support folders")
	}
	return f.MakeFolder(id, p)
}
func (m *Manager) MoveFolder(id, a, b string) error {
	c, e := m.store(id)
	if e != nil {
		return e
	}
	f, ok := c.(ports.FolderStore)
	if !ok {
		return domain.Unsupported("provider does not support folders")
	}
	return f.MoveFolder(id, a, b)
}
func (m *Manager) RemoveFolder(id, p string) error {
	c, e := m.store(id)
	if e != nil {
		return e
	}
	f, ok := c.(ports.FolderStore)
	if !ok {
		return domain.Unsupported("provider does not support folders")
	}
	return f.RemoveFolder(id, p)
}
func (m *Manager) StashFolder(id, p, t string, docs []string) error {
	c, e := m.store(id)
	if e != nil {
		return e
	}
	f, ok := c.(ports.FolderStore)
	if !ok {
		return domain.Unsupported("provider does not support folders")
	}
	return f.StashFolder(id, p, t, docs)
}
func (m *Manager) StashedFolders(id string) ([]ports.FolderTrashEntry, error) {
	c, e := m.store(id)
	if e != nil {
		return nil, e
	}
	f, ok := c.(ports.FolderStore)
	if !ok {
		return nil, domain.Unsupported("provider does not support folders")
	}
	return f.StashedFolders(id)
}
func (m *Manager) UnstashFolder(id, t string) (string, []string, error) {
	c, e := m.store(id)
	if e != nil {
		return "", nil, e
	}
	f, ok := c.(ports.FolderStore)
	if !ok {
		return "", nil, domain.Unsupported("provider does not support folders")
	}
	return f.UnstashFolder(id, t)
}
