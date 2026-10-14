package httpapi

import "net/http"

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		folders, err := s.Svc.ListFolders(r.Context(), actorOf(r), spaceID(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"folders": folders})
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Svc.CreateFolder(r.Context(), actorOf(r), spaceID(r), in.Path); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"path": in.Path})
}

func (s *Server) handleFolderRename(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path    string `json:"path"`
		NewPath string `json:"newPath"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.Svc.RenameFolder(r.Context(), actorOf(r), spaceID(r), in.Path, in.NewPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleFolderTrash(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.Svc.TrashFolder(r.Context(), actorOf(r), spaceID(r), in.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleFolderTrashes(w http.ResponseWriter, r *http.Request) {
	trashes, err := s.Svc.ListFolderTrashes(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"folders": trashes})
}

func (s *Server) handleFolderRestore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TrashID string `json:"trashId"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.Svc.RestoreFolder(r.Context(), actorOf(r), spaceID(r), in.TrashID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}
