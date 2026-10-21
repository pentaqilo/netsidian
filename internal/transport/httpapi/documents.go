package httpapi

import (
	"github.com/pentaqilo/netsidian/internal/domain"
	"net/http"
	"strconv"
	"strings"

	"github.com/pentaqilo/netsidian/internal/ports"
)

func (s *Server) handleDocsList(w http.ResponseWriter, r *http.Request) {
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 500 {
			writeErr(w, domain.Invalid("limit must be between 1 and 500"))
			return
		}
		if _, err = s.Svc.Require(r.Context(), actorOf(r), spaceID(r), domain.ActionRead); err != nil {
			writeErr(w, err)
			return
		}
		if repo, ok := s.Svc.Docs.(ports.DocumentPageRepo); ok {
			page, err := repo.DocumentMetadataPage(r.Context(), spaceID(r), r.URL.Query().Get("trashed") == "true", r.URL.Query().Get("cursor"), limit)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, 200, page)
			return
		}
	}
	docs, err := s.Svc.ListDocuments(r.Context(), actorOf(r), spaceID(r), r.URL.Query().Get("trashed") == "true")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"documents": docs})
}

func (s *Server) handleDocCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if !decode(w, r, &in) {
		return
	}
	doc, err := s.Svc.CreateNote(r.Context(), actorOf(r), spaceID(r), in.Path, in.Content,
		r.Header.Get("Idempotency-Key"), false)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, doc)
}

const documentReadGuide = "{doc} is a document ID, not a file path. Use GET /api/v1/spaces/{space}/documents/by-path?path=Research/Note.md for content, or GET /api/v1/spaces/{space}/documents/lookup?path=Research/Note.md then GET /api/v1/spaces/{space}/documents/{id}."

func (s *Server) handleDocGet(w http.ResponseWriter, r *http.Request) {
	id := docID(r)
	if strings.ContainsAny(id, "/\\") || strings.HasSuffix(strings.ToLower(id), ".md") {
		if _, err := s.Svc.Require(r.Context(), actorOf(r), spaceID(r), domain.ActionRead); err != nil {
			writeErr(w, err)
			return
		}
		writeErr(w, domain.Invalid(documentReadGuide))
		return
	}
	s.writeDocumentContent(w, r, id)
}

func (s *Server) handleDocByPath(w http.ResponseWriter, r *http.Request) {
	doc, err := s.Svc.LookupDocument(r.Context(), actorOf(r), spaceID(r), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	s.writeDocumentContent(w, r, doc.ID)
}

func (s *Server) writeDocumentContent(w http.ResponseWriter, r *http.Request, id string) {
	nc, err := s.Svc.ReadNote(r.Context(), actorOf(r), spaceID(r), id)
	if err != nil {
		if e, ok := err.(*domain.Error); ok && e.Code == domain.ErrCodeNotFound {
			err = domain.NotFound("document not found. " + documentReadGuide)
		}
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+nc.Revision+`"`)
	writeJSON(w, 200, map[string]any{"documentId": id, "path": nc.Path, "kind": nc.Kind, "title": nc.Title, "markdown": nc.Markdown, "revision": nc.Revision, "canEdit": nc.CanEdit})
}

func (s *Server) handleDocSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Markdown *string `json:"markdown"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Markdown == nil {
		writeErr(w, domain.Invalid("markdown is required for updates; document creation uses content. No changes were saved."))
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	res, err := s.Svc.SaveContent(r.Context(), actorOf(r), spaceID(r), docID(r), ifMatch, *in.Markdown,
		r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+res.Revision+`"`)
	writeJSON(w, 200, res)
}

func (s *Server) handleDocRename(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &in) {
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	if err := s.Svc.Rename(r.Context(), actorOf(r), spaceID(r), docID(r), in.Path, ifMatch); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDocTrash(w http.ResponseWriter, r *http.Request) {
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	if err := s.Svc.Trash(r.Context(), actorOf(r), spaceID(r), docID(r), ifMatch); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDocRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.Restore(r.Context(), actorOf(r), spaceID(r), docID(r)); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleRevisions(w http.ResponseWriter, r *http.Request) {
	revs, err := s.Svc.Revisions(r.Context(), actorOf(r), spaceID(r), docID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"revisions": revs})
}

func (s *Server) handleRevisionGet(w http.ResponseWriter, r *http.Request) {
	md, err := s.Svc.RevisionContent(r.Context(), actorOf(r), spaceID(r), docID(r), r.PathValue("rev"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"revisionId": r.PathValue("rev"), "markdown": md})
}

func (s *Server) handleBacklinks(w http.ResponseWriter, r *http.Request) {
	bl, err := s.Svc.Backlinks(r.Context(), actorOf(r), spaceID(r), docID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"backlinks": bl})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := &ports.SearchQuery{
		SpaceID:      spaceID(r),
		Text:         r.URL.Query().Get("q"),
		Tags:         splitCSV(r.URL.Query().Get("tags")),
		PathHas:      r.URL.Query().Get("path"),
		FolderPrefix: r.URL.Query().Get("folder"),
		TagMode:      r.URL.Query().Get("tagMode"),
		Limit:        intQuery(r, "limit", 25),
		Offset:       intQuery(r, "offset", 0),
	}
	hits, more, err := s.Svc.SearchPage(r.Context(), actorOf(r), q)
	if err != nil {
		writeErr(w, err)
		return
	}
	if hits == nil {
		hits = []*ports.SearchHit{}
	}
	writeJSON(w, 200, map[string]any{"hits": hits, "limit": q.Limit, "offset": q.Offset, "hasMore": more})
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	g, err := s.Svc.Graph(r.Context(), actorOf(r), spaceID(r), intQuery(r, "limit", 300))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, g)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.Svc.Tags(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"tags": tags})
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) handleDocLookup(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, domain.Invalid("path is required"))
		return
	}
	doc, err := s.Svc.LookupDocument(r.Context(), actorOf(r), spaceID(r), path)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+doc.CurrentRevision+`"`)
	writeJSON(w, 200, map[string]any{"id": doc.ID, "path": doc.Path, "kind": doc.Kind, "currentRevision": doc.CurrentRevision, "updatedAt": doc.UpdatedAt})
}
