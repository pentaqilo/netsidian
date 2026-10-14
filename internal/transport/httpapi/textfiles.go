package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func (s *Server) handleTextFileGet(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("path")
	if raw == "" {
		writeErr(w, domain.Invalid("file path required"))
		return
	}
	data, etag, err := s.Svc.ReadTextFile(r.Context(), actorOf(r), spaceID(r), raw)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+etag+`"`)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"path": raw, "content": string(data), "etag": etag})
}

func (s *Server) handleTextFilePut(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("path")
	if raw == "" {
		writeErr(w, domain.Invalid("file path required"))
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			writeErr(w, domain.TooLarge("file exceeds the upload limit"))
			return
		}
		writeErr(w, domain.Invalid("file body could not be read"))
		return
	}
	meta, etag, err := s.Svc.WriteTextFile(r.Context(), actorOf(r), spaceID(r), raw, data, r.Header.Get("If-Match"))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("ETag", `"`+etag+`"`)
	writeJSON(w, 201, meta)
}
