package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// MoveAttachment uses the same vault lock as imports and folder operations.
// It refuses an occupied target and preserves the attachment's byte type.
func (s *Services) MoveAttachment(ctx context.Context, a *Actor, space, from, to string) error {
	if _, err := s.Require(ctx, a, space, domain.ActionAttachments); err != nil {
		return err
	}
	if a.Key != nil && len(a.Key.Scopes) > 0 && !hasScope(a.Key.Scopes, domain.ScopeAttachmentsWrite) {
		return domain.Forbidden("key lacks attachments:write")
	}
	source, err := validateAttachmentPath(from)
	if err != nil {
		return err
	}
	target, err := validateAttachmentPath(to)
	if err != nil {
		return err
	}
	oldType, _ := attachmentKind(source)
	newType, _ := attachmentKind(target)
	if oldType != newType {
		return domain.Invalid("moving an attachment cannot change its file type")
	}
	if source == target {
		return nil
	}
	mu := s.spaceMu(space)
	mu.Lock()
	defer mu.Unlock()
	list, ok := s.Content.(ports.AttachmentStore)
	if !ok {
		return domain.Unsupported("storage cannot verify attachment moves")
	}
	files, err := list.ListFiles(space, source)
	if err != nil {
		return err
	}
	exists := false
	for _, f := range files {
		if f.Path == source {
			exists = true
			break
		}
	}
	if !exists {
		return domain.NotFound("attachment not found")
	}
	if exists, err := s.Content.Exists(space, target); err != nil {
		return err
	} else if exists {
		return domain.Conflict("a file already exists at the destination")
	}
	if err := s.Content.Move(space, source, target); err != nil {
		return err
	}
	s.PublishEvent(&domain.Event{SpaceID: space, EventType: "attachment.moved"})
	return nil
}
