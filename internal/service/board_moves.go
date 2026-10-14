package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// The existing journaled rename preserves identity/history and refuses an
// occupied destination. Link repairs use revision preconditions and report
// partial failures rather than overwriting concurrent document edits.
func (s *Services) moveBoardDocument(ctx context.Context, a *Actor, space string, doc *domain.Document, source *NoteContent, next, revision string) ([]string, error) {
	if next == source.Path {
		return nil, nil
	}
	docs, err := s.ListDocuments(ctx, a, space, false)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(docs))
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	if store, ok := s.Content.(ports.AttachmentStore); ok {
		files, err := store.ListFiles(space, "")
		if err != nil {
			return nil, domain.Unavailable("could not inspect attachment paths before moving")
		}
		for _, file := range files {
			paths = append(paths, file.Path)
		}
	}
	type repair struct {
		id   string
		note *NoteContent
		text string
	}
	var repairs []repair
	for _, d := range docs {
		note, err := s.ReadNote(ctx, a, space, d.ID)
		if err != nil {
			return nil, err
		}
		text := markdown.RewriteMovedLinks(note.Markdown, note.Path, source.Path, next, paths)
		if text != note.Markdown {
			repairs = append(repairs, repair{d.ID, note, text})
		}
	}
	if err := s.Rename(ctx, a, space, doc.ID, next, revision, source.Path); err != nil {
		return nil, err
	}
	var warnings []string
	for _, r := range repairs {
		if _, err := s.SaveContent(ctx, a, space, r.id, r.note.Revision, r.text, "", shaHex([]byte(r.note.Markdown))); err != nil {
			warnings = append(warnings, "Moved task, but links in "+r.note.Path+" could not be updated: "+err.Error())
		}
	}
	return warnings, nil
}
