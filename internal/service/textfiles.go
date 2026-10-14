package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// textFiles are editable plain-text vault members (diagram sources, vector
// sources) stored on the same content store as attachments but edited with
// optimistic-concurrency semantics like documents.
var textFileMIME = map[string]string{
	".puml":     "text/plain; charset=utf-8",
	".plantuml": "text/plain; charset=utf-8",
	".mmd":      "text/plain; charset=utf-8",
	".svg":      "image/svg+xml",
}

const textFileLimit = 2 << 20

func textFileKind(rel string) (string, bool) {
	mime, ok := textFileMIME[strings.ToLower(filepath.Ext(rel))]
	return mime, ok
}

func validateTextFilePath(path string) (string, error) {
	canon, _, err := domain.NormalizePath(path)
	if err != nil {
		return "", err
	}
	for _, part := range strings.Split(canon, "/") {
		if strings.HasPrefix(part, ".") {
			return "", domain.Invalid("reserved file name")
		}
	}
	if _, ok := textFileKind(canon); !ok {
		return "", domain.Invalid("editable file type not allowed")
	}
	return canon, nil
}

func textFileETag(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256-" + hex.EncodeToString(sum[:])
}

// ReadTextFile returns current bytes and their etag for If-Match editing.
func (s *Services) ReadTextFile(ctx context.Context, a *Actor, space, path string) ([]byte, string, error) {
	if err := s.attachmentReadGate(ctx, a, space); err != nil {
		return nil, "", err
	}
	canon, err := validateTextFilePath(path)
	if err != nil {
		return nil, "", err
	}
	data, err := s.Content.Read(space, canon)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", domain.NotFound("file not found")
		}
		return nil, "", domain.Unavailable("file storage failed")
	}
	return data, textFileETag(data), nil
}

// WriteTextFile stores text after permission, size and If-Match checks.
// "*" (or no etag when the file does not exist yet) creates or overwrites
// deliberately; an existing file requires the exact current etag.
func (s *Services) WriteTextFile(ctx context.Context, a *Actor, space, path string, data []byte, ifMatch string) (*AttachmentMeta, string, error) {
	if _, err := s.Require(ctx, a, space, domain.ActionAttachments); err != nil {
		return nil, "", err
	}
	if a.Key != nil && len(a.Key.Scopes) > 0 && !hasScope(a.Key.Scopes, domain.ScopeAttachmentsWrite) {
		return nil, "", domain.Forbidden("key lacks attachments:write")
	}
	canon, err := validateTextFilePath(path)
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > textFileLimit {
		return nil, "", domain.TooLarge("file exceeds the editable size limit")
	}
	ifMatch = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(ifMatch), "W/"), "\"")
	ifMatch = strings.Trim(ifMatch, "\"")
	_, key, _ := domain.NormalizePath(canon)
	existing, err := s.Docs.DocumentByNormKey(ctx, space, key)
	if err != nil {
		return nil, "", err
	}
	if existing != nil {
		return nil, "", domain.Conflict("a document already exists at that path")
	}
	smu := s.spaceMu(space)
	smu.Lock()
	defer smu.Unlock()
	current, readErr := s.Content.Read(space, canon)
	if ifMatch != "*" && !(readErr != nil && os.IsNotExist(readErr)) {
		if readErr != nil {
			return nil, "", domain.Unavailable("file storage failed")
		}
		if textFileETag(current) != ifMatch {
			return nil, "", domain.Conflict("file changed on the server; reload before saving")
		}
	}
	if err := s.Content.WriteAtomic(space, canon, data); err != nil {
		return nil, "", domain.Unavailable("file storage failed")
	}
	mime, _ := textFileKind(canon)
	s.PublishEvent(&domain.Event{SpaceID: space, EventType: "textfile.saved"})
	return &AttachmentMeta{Path: canon, Size: int64(len(data)), ContentType: mime}, textFileETag(data), nil
}
