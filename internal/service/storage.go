package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func (s *Services) StorageOptions() []string {
	if m, ok := s.Content.(ports.VaultStorageManager); ok {
		return m.Providers()
	}
	return []string{"base"}
}
func (s *Services) VaultStorage(ctx context.Context, a *Actor, id string) (*ports.StorageConfig, error) {
	if a == nil || !a.Human() {
		return nil, domain.Forbidden("storage management requires a human session")
	}
	if _, err := s.Require(ctx, a, id, domain.ActionSpaceManage); err != nil {
		return nil, err
	}
	if m, ok := s.Content.(ports.VaultStorageManager); ok {
		return m.Describe(ctx, id)
	}
	return &ports.StorageConfig{Provider: "base"}, nil
}
func (s *Services) ConfigureVaultStorage(ctx context.Context, a *Actor, id string, v ports.StorageConfig) error {
	if a == nil || !a.Human() {
		return domain.Forbidden("storage management requires a human session")
	}
	if _, err := s.Require(ctx, a, id, domain.ActionSpaceManage); err != nil {
		return err
	}
	lock := s.spaceMu(id)
	lock.Lock()
	defer lock.Unlock()
	sp, err := s.Spaces.Space(ctx, id)
	if err != nil {
		return err
	}
	if sp == nil {
		return domain.NotFound("vault not found")
	}
	m, ok := s.Content.(ports.VaultStorageManager)
	if !ok {
		return domain.Unsupported("vault storage providers unavailable")
	}
	old, err := m.Describe(ctx, id)
	if err != nil {
		return err
	}
	if old.Provider == "base" {
		if sp.StorageBinding != "local-main" && sp.StorageBinding != "s3-main" {
			return domain.Conflict("attached folders cannot change storage provider")
		}
		docs, err := s.Docs.DocumentsInSpace(ctx, id, true)
		if err != nil {
			return err
		}
		if len(docs) > 0 {
			return domain.Conflict("choose storage for an empty vault; content migration is not automatic")
		}
		if files, ok := s.Content.(ports.AttachmentStore); ok {
			f, e := files.ListFiles(id, "")
			if e != nil {
				return e
			}
			if len(f) > 0 {
				return domain.Conflict("vault contains files")
			}
		}
		if folders, ok := s.Content.(ports.FolderStore); ok {
			f, e := folders.Folders(id)
			if e != nil {
				return e
			}
			if len(f) > 0 {
				return domain.Conflict("vault contains folders")
			}
		}
	}
	if err = m.Configure(ctx, id, a.UserID, v); err != nil {
		return domain.Invalid(err.Error())
	}
	return nil
}
