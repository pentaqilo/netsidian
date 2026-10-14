package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// VaultExportSummary describes a completed (or aborted) export stream.
type VaultExportSummary struct {
	Vault       string `json:"vaultId"`
	Name        string `json:"name"`
	Documents   int    `json:"documents"`
	Attachments int    `json:"attachments"`
	Folders     int    `json:"folders"`
	Bytes       int64  `json:"bytes"`
}

// VaultForExport authorizes an export and returns the vault. Handlers
// call it before writing any response bytes so authorization failures
// produce a proper error instead of a half-streamed archive.
func (s *Services) VaultForExport(ctx context.Context, a *Actor, spaceID string) (*domain.Space, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionExport); err != nil {
		return nil, err
	}
	return s.Spaces.Space(ctx, spaceID)
}

// ExportVault streams the vault as ordinary folders and files with
// vault-relative paths: every note and attachment at its exact path,
// empty folders as directory entries, and a single separable metadata
// manifest under netsidian/. Search indexes, graph positions, concepts
// and clusters are derived data and intentionally not exported. The
// context is honoured so an aborted download stops the walk.
func (s *Services) ExportVault(ctx context.Context, a *Actor, spaceID string, zw *zip.Writer) (*VaultExportSummary, error) {
	sp, err := s.VaultForExport(ctx, a, spaceID)
	if err != nil {
		return nil, err
	}
	docs, err := s.ListDocuments(ctx, a, spaceID, false)
	if err != nil {
		return nil, err
	}
	atts, err := s.ListAttachments(ctx, a, spaceID, "")
	if err != nil {
		return nil, err
	}
	folders, err := s.ListFolders(ctx, a, spaceID)
	if err != nil {
		return nil, err
	}
	sum := &VaultExportSummary{Vault: spaceID, Name: sp.Name,
		Documents: len(docs), Attachments: len(atts), Folders: len(folders)}

	add := func(name string, data []byte) error {
		hw := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hw.Modified = time.Now().UTC().Truncate(2 * time.Second)
		fw, err := zw.CreateHeader(hw)
		if err != nil {
			return err
		}
		n, err := fw.Write(data)
		sum.Bytes += int64(n)
		return err
	}
	addDir := func(name string) error {
		hw := &zip.FileHeader{Name: strings.TrimSuffix(name, "/") + "/", Method: zip.Store}
		_, err := zw.CreateHeader(hw)
		return err
	}

	manifest := map[string]any{
		"tool": "netsidian", "exportVersion": 1,
		"vaultId": spaceID, "name": sp.Name, "kind": sp.Kind,
		"exportedAt": time.Now().UTC().Format(time.RFC3339),
		"documents":  len(docs), "attachments": len(atts), "folders": len(folders),
		"note": "Netsidian metadata only. Markdown, folders and attachments at vault-relative paths are the source of truth; delete netsidian/ to obtain a plain vault tree.",
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := add("netsidian/vault.json", raw); err != nil {
		return nil, err
	}
	for _, f := range folders {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := addDir(f); err != nil {
			return nil, err
		}
	}
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nc, err := s.ReadNote(ctx, a, spaceID, d.ID)
		if err != nil {
			return nil, err
		}
		if err := add(d.Path, []byte(nc.Markdown)); err != nil {
			return nil, err
		}
	}
	for _, at := range atts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, _, err := s.ReadAttachment(ctx, a, spaceID, at.Path)
		if err != nil {
			return nil, err
		}
		if err := add(at.Path, data); err != nil {
			return nil, err
		}
	}
	return sum, nil
}
