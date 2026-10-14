package service

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// attachmentMIME is the closed allowlist of attachment types: Obsidian
// embed targets plus PDF. Anything else is rejected before it touches
// storage, so served bytes are always one of these content types.
var attachmentMIME = map[string]string{
	".png":      "image/png",
	".jpg":      "image/jpeg",
	".jpeg":     "image/jpeg",
	".gif":      "image/gif",
	".webp":     "image/webp",
	".svg":      "image/svg+xml",
	".pdf":      "application/pdf",
	".mp4":      "video/mp4",
	".puml":     "text/plain; charset=utf-8",
	".plantuml": "text/plain; charset=utf-8",
	".mmd":      "text/plain; charset=utf-8",
}

// attachmentSniff maps each allowed extension to the content types the
// sniffer may legitimately report for those bytes.
var attachmentSniff = map[string]map[string]bool{
	".png":      {"image/png": true},
	".jpg":      {"image/jpeg": true},
	".jpeg":     {"image/jpeg": true},
	".gif":      {"image/gif": true},
	".webp":     {"image/webp": true},
	".svg":      {"text/xml": true, "application/xml": true},
	".pdf":      {"application/pdf": true},
	".mp4":      {"video/mp4": true, "application/octet-stream": true},
	".puml":     {"text/plain": true, "application/octet-stream": true},
	".plantuml": {"text/plain": true, "application/octet-stream": true},
	".mmd":      {"text/plain": true, "application/octet-stream": true},
}

func attachmentKind(rel string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(rel))
	mime, ok := attachmentMIME[ext]
	return mime, ok
}

func attachmentLimit(s *Services) int64 {
	if s.Cfg.MaxAttachBytes > 0 {
		return s.Cfg.MaxAttachBytes
	}
	return 26214400
}

// AttachmentMeta describes one stored attachment.
type AttachmentMeta struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
}

func validateAttachmentPath(path string) (string, error) {
	canon, _, err := domain.NormalizePath(path)
	if err != nil {
		return "", err
	}
	for _, part := range strings.Split(canon, "/") {
		if strings.HasPrefix(part, ".") {
			return "", domain.Invalid("reserved attachment name")
		}
	}
	if _, ok := attachmentKind(canon); !ok {
		return "", domain.Invalid("attachment type not allowed")
	}
	return canon, nil
}

// PutAttachment stores bytes at an attachment path after extension,
// size and content-sniffing checks. It never overwrites a document path.
func (s *Services) PutAttachment(ctx context.Context, a *Actor, space, path string, data []byte) (*AttachmentMeta, error) {
	return s.putAttachment(ctx, a, space, path, data, false)
}

// PutAttachmentIfAbsent protects copied/imported files from racing app uploads.
func (s *Services) PutAttachmentIfAbsent(ctx context.Context, a *Actor, space, path string, data []byte) (*AttachmentMeta, error) {
	return s.putAttachment(ctx, a, space, path, data, true)
}
func (s *Services) putAttachment(ctx context.Context, a *Actor, space, path string, data []byte, onlyIfAbsent bool) (*AttachmentMeta, error) {

	if _, err := s.Require(ctx, a, space, domain.ActionAttachments); err != nil {
		return nil, err
	}
	if a.Key != nil && len(a.Key.Scopes) > 0 && !hasScope(a.Key.Scopes, domain.ScopeAttachmentsWrite) {
		return nil, domain.Forbidden("key lacks attachments:write")
	}
	canon, err := validateAttachmentPath(path)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > attachmentLimit(s) {
		return nil, domain.TooLarge("attachment exceeds the configured size limit")
	}
	if len(data) == 0 {
		return nil, domain.Invalid("attachment is empty")
	}
	ext := strings.ToLower(filepath.Ext(canon))
	sniffed := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0]))
	if !attachmentSniff[ext][sniffed] {
		return nil, domain.Invalid("file contents do not match the attachment type")
	}
	_, key, _ := domain.NormalizePath(canon)
	existing, err := s.Docs.DocumentByNormKey(ctx, space, key)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, domain.Conflict("a document already exists at that path")
	}
	// Serialize with saves, renames and folder operations: an attachment
	// write must never land into (or race with) a moving tree.
	smu := s.spaceMu(space)
	smu.Lock()
	defer smu.Unlock()
	if onlyIfAbsent {
		if exists, err := s.Content.Exists(space, canon); err != nil {
			return nil, err
		} else if exists {
			return nil, domain.Conflict("an attachment already exists at that path")
		}
	}
	if err := s.Content.WriteAtomic(space, canon, data); err != nil {
		if os.IsNotExist(err) {
			return nil, domain.Unavailable("attachment storage failed")
		}
		return nil, domain.Unavailable("attachment storage failed")
	}
	mime, _ := attachmentKind(canon)
	s.PublishEvent(&domain.Event{SpaceID: space, EventType: "attachment.created"})
	return &AttachmentMeta{Path: canon, Size: int64(len(data)), ContentType: mime}, nil
}

// attachmentReadGate checks attachment-read permission. Key-based access
// is governed by the dedicated attachments:read scope (an empty scope
// list inherits the holder's role, matching Require semantics); human
// sessions follow the normal read permission.
func (s *Services) attachmentReadGate(ctx context.Context, a *Actor, space string) error {
	if a.Key == nil {
		_, err := s.Require(ctx, a, space, domain.ActionRead)
		return err
	}
	role, err := s.Role(ctx, a, space)
	if err != nil {
		return err
	}
	if !domain.RoleCan(role, domain.ActionRead) {
		return domain.Forbidden("role " + string(role) + " may not read attachments")
	}
	inSpace := false
	for _, id := range a.Key.SpaceIDs {
		if id == space {
			inSpace = true
			break
		}
	}
	if !inSpace {
		return domain.Forbidden("key is not bound to this space")
	}
	if len(a.Key.Scopes) > 0 && !hasScope(a.Key.Scopes, domain.ScopeAttachmentsRead) {
		return domain.Forbidden("key lacks attachments:read")
	}
	return nil
}

// ReadAttachment returns validated attachment bytes and their content type.
// Key-based access must additionally carry the attachments:read scope.
func (s *Services) ReadAttachment(ctx context.Context, a *Actor, space, path string) ([]byte, string, error) {
	if err := s.attachmentReadGate(ctx, a, space); err != nil {
		return nil, "", err
	}
	canon, err := validateAttachmentPath(path)
	if err != nil {
		return nil, "", err
	}
	data, err := s.Content.Read(space, canon)
	if err != nil {
		return nil, "", domain.NotFound("attachment not found")
	}
	mime, _ := attachmentKind(canon)
	return data, mime, nil
}

// ListAttachments lists stored attachments under a prefix ("" = whole
// space) for import collision checks and folder views.
func (s *Services) ListAttachments(ctx context.Context, a *Actor, space, prefix string) ([]*AttachmentMeta, error) {
	if err := s.attachmentReadGate(ctx, a, space); err != nil {
		return nil, err
	}
	store, ok := s.Content.(ports.AttachmentStore)
	if !ok {
		return []*AttachmentMeta{}, nil
	}
	canonPrefix := ""
	if prefix != "" && prefix != "." {
		p, _, err := domain.NormalizePath(prefix)
		if err != nil {
			return nil, err
		}
		canonPrefix = p
	}
	entries, err := store.ListFiles(space, canonPrefix)
	if err != nil {
		return nil, domain.Unavailable("attachment listing failed")
	}
	out := []*AttachmentMeta{}
	for _, e := range entries {
		mime, ok := attachmentKind(e.Path)
		if !ok {
			continue
		}
		out = append(out, &AttachmentMeta{Path: e.Path, Size: e.Size, ContentType: mime})
	}
	return out, nil
}
