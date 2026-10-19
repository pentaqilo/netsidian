package httpapi

import (
	"net/http"
	"strings"

	"github.com/pentaqilo/netsidian/internal/service"
)

// Folder-backed mind map endpoints. The map folder is addressed with the
// `path` query parameter so canvas/node/connector sub-operations stay on
// literal routes (no wildcard-suffix ambiguity in the mux).

func mindMapPath(r *http.Request) string {
	return r.URL.Query().Get("path")
}

func (s *Server) handleMindMapsList(w http.ResponseWriter, r *http.Request) {
	maps, err := s.Svc.ListMindMaps(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"mindMaps": maps})
}

func (s *Server) handleMindMapGet(w http.ResponseWriter, r *http.Request) {
	m, err := s.Svc.GetMindMap(r.Context(), actorOf(r), spaceID(r), mindMapPath(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+m.Revision+`"`)
	writeJSON(w, 200, m)
}

func (s *Server) handleMindMapCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.Svc.CreateMindMap(r.Context(), actorOf(r), spaceID(r), in.Path, in.Title,
		r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, m)
}

func (s *Server) handleMindMapConfig(w http.ResponseWriter, r *http.Request) {
	var in service.UpdateMindMapConfigRequest
	if !decode(w, r, &in) {
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	m, err := s.Svc.UpdateMindMapConfig(r.Context(), actorOf(r), spaceID(r), mindMapPath(r), in, ifMatch)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+m.Revision+`"`)
	writeJSON(w, 200, m)
}

func (s *Server) handleMindMapNodeCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string  `json:"title"`
		X     float64 `json:"x"`
		Y     float64 `json:"y"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.Svc.CreateMindMapNode(r.Context(), actorOf(r), spaceID(r), mindMapPath(r), in.Title, in.X, in.Y,
		r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, m)
}

func (s *Server) handleMindMapNodeReference(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DocID string  `json:"docId"`
		X     float64 `json:"x"`
		Y     float64 `json:"y"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.Svc.AddMindMapReference(r.Context(), actorOf(r), spaceID(r), mindMapPath(r), in.DocID, in.X, in.Y)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, m)
}

func (s *Server) handleMindMapNodeMove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DocID    string `json:"docId"`
		FromPath string `json:"fromPath"`
	}
	if !decode(w, r, &in) {
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	m, warnings, err := s.Svc.MoveDocIntoMindMap(r.Context(), actorOf(r), spaceID(r), mindMapPath(r), in.DocID, ifMatch, in.FromPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"mindMap": m, "warnings": warnings})
}

func (s *Server) handleMindMapNodeDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NodeID string `json:"nodeId"`
		Trash  bool   `json:"trash"`
	}
	if !decode(w, r, &in) {
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	m, err := s.Svc.DeleteMindMapNode(r.Context(), actorOf(r), spaceID(r), mindMapPath(r), in.NodeID, ifMatch, in.Trash)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+m.Revision+`"`)
	writeJSON(w, 200, m)
}

func (s *Server) handleMindMapNodeFiles(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NodeID   string `json:"nodeId"`
		DocID    string `json:"docId"`
		FromPath string `json:"fromPath"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, warnings, err := s.Svc.MoveFileIntoMindMapNode(r.Context(), actorOf(r), spaceID(r), mindMapPath(r), in.NodeID, in.DocID, strings.Trim(r.Header.Get("If-Match"), `"`), in.FromPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"mindMap": m, "warnings": warnings})
}

func (s *Server) handleMindMapNodeLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NodeID         string `json:"nodeId"`
		DocID          string `json:"docId"`
		AttachmentPath string `json:"attachmentPath"`
	}
	if !decode(w, r, &in) {
		return
	}
	m, err := s.Svc.LinkFileToMindMapNode(r.Context(), actorOf(r), spaceID(r), mindMapPath(r), in.NodeID, in.DocID, in.AttachmentPath, strings.Trim(r.Header.Get("If-Match"), `"`))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, m)
}
