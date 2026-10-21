package service

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// DocumentChunk is revision-pinned and bounded by serialized UTF-8 JSON bytes.
type DocumentChunk struct {
	DocumentID string `json:"documentId"`
	Path       string `json:"path"`
	Revision   string `json:"revision"`
	CanEdit    bool   `json:"canEdit"`
	Markdown   string `json:"markdown"`
	Offset     int    `json:"offset"`
	TotalBytes int    `json:"totalBytes"`
	HasMore    bool   `json:"hasMore"`
	NextOffset int    `json:"nextOffset,omitempty"`
}

func (s *Services) ReadDocumentChunk(ctx context.Context, a *Actor, space, id, documentPath, revision string, offset, maxBytes int) (*DocumentChunk, error) {
	if (id == "") == (documentPath == "") {
		return nil, domain.Invalid("provide exactly one of documentId or path")
	}
	if maxBytes == 0 {
		maxBytes = 8192
	}
	if maxBytes < 1024 || maxBytes > 32768 {
		return nil, domain.Invalid("maxBytes must be 1024..32768")
	}
	if offset > 0 && revision == "" {
		return nil, domain.Invalid("revision from the first read is required with offset")
	}
	if id == "" {
		doc, err := s.LookupDocument(ctx, a, space, documentPath)
		if err != nil {
			return nil, err
		}
		id = doc.ID
	}
	note, err := s.ReadNote(ctx, a, space, id)
	if err != nil {
		return nil, err
	}
	if revision != "" && revision != note.Revision {
		return nil, domain.PreconditionFailed(note.Revision)
	}
	if offset < 0 || offset > len(note.Markdown) || (offset < len(note.Markdown) && !utf8.RuneStart(note.Markdown[offset])) {
		return nil, domain.Invalid("offset must be a UTF-8 boundary within totalBytes")
	}
	out := &DocumentChunk{DocumentID: id, Path: note.Path, Revision: note.Revision, CanEdit: note.CanEdit, Offset: offset, TotalBytes: len(note.Markdown)}
	setEnd := func(end int) {
		out.Markdown = note.Markdown[offset:end]
		out.HasMore = end < len(note.Markdown)
		out.NextOffset = 0
		if out.HasMore {
			out.NextOffset = end
		}
	}
	lo, hi := offset, min(offset+maxBytes, len(note.Markdown))
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		setEnd(mid)
		data, _ := json.Marshal(out)
		if len(data) <= maxBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	end := lo
	for end > offset && end < len(note.Markdown) && !utf8.RuneStart(note.Markdown[end]) {
		end--
	}
	setEnd(end)
	data, _ := json.Marshal(out)
	if len(data) > maxBytes || (end == offset && offset < len(note.Markdown)) {
		return nil, domain.TooLarge("document metadata or next character exceeds maxBytes; increase the budget")
	}
	return out, nil
}
