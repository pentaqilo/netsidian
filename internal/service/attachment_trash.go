package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"strings"
)

func (s *Services) TrashAttachment(ctx context.Context, a *Actor, space, path string) (*ports.FolderTrashEntry, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionTrash); err != nil {
		return nil, err
	}
	if a.Key != nil && len(a.Key.Scopes) > 0 && !hasScope(a.Key.Scopes, domain.ScopeAttachmentsWrite) {
		return nil, domain.Forbidden("key lacks attachments:write")
	}
	sp, err := s.Spaces.Space(ctx, space)
	if err != nil {
		return nil, err
	}
	if sp == nil {
		return nil, domain.NotFound("vault not found")
	}
	if strings.HasPrefix(sp.StorageBinding, "local-folder:") {
		return nil, domain.Unsupported("attached folders do not support app trash")
	}
	canon, err := validateAttachmentPath(path)
	if err != nil {
		return nil, err
	}
	store, ok := s.Content.(ports.AttachmentTrashStore)
	if !ok {
		return nil, domain.Unsupported("storage does not support attachment trash")
	}
	mu := s.spaceMu(space)
	mu.Lock()
	defer mu.Unlock()
	id := domain.NewID("ftr")
	if err := store.StashAttachment(space, canon, id); err != nil {
		return nil, err
	}
	s.PublishEvent(&domain.Event{SpaceID: space, EventType: "attachment.trashed"})
	return &ports.FolderTrashEntry{TrashID: id, Path: canon, Kind: "attachment"}, nil
}
