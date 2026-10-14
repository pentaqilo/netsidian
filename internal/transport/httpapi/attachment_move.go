package httpapi

import "net/http"

func (s *Server) handleAttachmentMove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Svc.MoveAttachment(r.Context(), actorOf(r), spaceID(r), in.From, in.To); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
