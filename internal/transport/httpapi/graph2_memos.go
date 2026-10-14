package httpapi

import (
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"net/http"
	"time"
)

func (s *Server) handleGraph2(w http.ResponseWriter, r *http.Request) {
	g, err := s.Svc.Graph2(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, g)
}
func (s *Server) handleMemos(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	q := &ports.MemoQuery{SpaceID: spaceID(r), Text: v.Get("q"), Tag: v.Get("tag"), Limit: intQuery(r, "limit", 20)}
	if v.Has("folder") {
		folder := v.Get("folder")
		q.Folder = &folder
	}
	for _, field := range []struct {
		name string
		dest *time.Time
	}{{"from", &q.From}, {"before", &q.Before}} {
		if value := v.Get(field.name); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				writeErr(w, domain.Invalid(field.name+" must be an RFC3339 timestamp"))
				return
			}
			*field.dest = parsed
		}
	}
	result, err := s.Svc.Memos(r.Context(), actorOf(r), q, v.Get("cursor"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, result)
}
