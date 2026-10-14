package providers

import (
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func (m *Manager) StashAttachment(space, path, id string) error {
	store, err := m.store(space)
	if err != nil {
		return err
	}
	trash, ok := store.(ports.AttachmentTrashStore)
	if !ok {
		return domain.Unsupported("provider does not support attachment trash")
	}
	return trash.StashAttachment(space, path, id)
}
