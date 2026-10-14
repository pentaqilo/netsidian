package httpapi

import "net/http"

func (s *Server) handleAttachmentTrash(w http.ResponseWriter, r *http.Request) {
	path, err := attachmentPath(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	entry, err := s.Svc.TrashAttachment(r.Context(), actorOf(r), spaceID(r), path)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}
