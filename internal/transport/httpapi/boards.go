package httpapi

import (
	"net/http"
	"strings"

	"github.com/pentaqilo/netsidian/internal/service"
)

// Folder-backed Kanban board endpoints. The board folder is addressed with
// the `path` query parameter so column/config/task sub-operations stay on
// literal routes (no wildcard-suffix ambiguity in the mux).

func boardPath(r *http.Request) string {
	return r.URL.Query().Get("path")
}

func (s *Server) handleBoardsList(w http.ResponseWriter, r *http.Request) {
	boards, err := s.Svc.ListBoards(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"boards": boards})
}

func (s *Server) handleBoardGet(w http.ResponseWriter, r *http.Request) {
	board, err := s.Svc.GetBoard(r.Context(), actorOf(r), spaceID(r), boardPath(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+board.Revision+`"`)
	writeJSON(w, 200, board)
}

func (s *Server) handleBoardCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path     string                `json:"path"`
		Title    string                `json:"title"`
		Columns  []service.BoardColumn `json:"columns"`
		Template string                `json:"template"`
	}
	if !decode(w, r, &in) {
		return
	}
	board, err := s.Svc.CreateBoard(r.Context(), actorOf(r), spaceID(r), in.Path, in.Columns, in.Title, in.Template,
		r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, board)
}

func (s *Server) handleBoardConfig(w http.ResponseWriter, r *http.Request) {
	var in service.UpdateBoardConfigRequest
	if !decode(w, r, &in) {
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	board, err := s.Svc.UpdateBoardConfig(r.Context(), actorOf(r), spaceID(r), boardPath(r), in, ifMatch)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+board.Revision+`"`)
	writeJSON(w, 200, board)
}

func (s *Server) handleBoardTaskCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ColumnID string `json:"columnId"`
		Title    string `json:"title"`
		Content  string `json:"content"`
	}
	if !decode(w, r, &in) {
		return
	}
	card, err := s.Svc.CreateTask(r.Context(), actorOf(r), spaceID(r), boardPath(r), in.ColumnID, in.Title, in.Content,
		r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, card)
}

func (s *Server) handleBoardTaskMove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DocID    string `json:"docId"`
		ColumnID string `json:"columnId"`
		FromPath string `json:"fromPath"`
	}
	if !decode(w, r, &in) {
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	card, err := s.Svc.MoveTask(r.Context(), actorOf(r), spaceID(r), boardPath(r), in.DocID, in.ColumnID, ifMatch, in.FromPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, card)
}

func (s *Server) handleBoardReorder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ColumnID   string   `json:"columnId"`
		OrderedIDs []string `json:"orderedIds"`
	}
	if !decode(w, r, &in) {
		return
	}
	ifMatch := strings.Trim(r.Header.Get("If-Match"), `"`)
	board, err := s.Svc.ReorderCards(r.Context(), actorOf(r), spaceID(r), boardPath(r), in.ColumnID, in.OrderedIDs, ifMatch)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+board.Revision+`"`)
	writeJSON(w, 200, board)
}
