package httpapi

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

func attachmentPath(r *http.Request) (string, error) {
	// ServeMux already percent-decodes path values; decoding twice would
	// corrupt names that legitimately contain a percent sign.
	raw := r.PathValue("path")
	if raw == "" {
		return "", domain.Invalid("attachment path required")
	}
	return raw, nil
}

func (s *Server) handleAttachmentPut(w http.ResponseWriter, r *http.Request) {
	p, err := attachmentPath(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			writeErr(w, domain.TooLarge("attachment exceeds the upload limit"))
			return
		}
		writeErr(w, domain.Invalid("attachment body could not be read"))
		return
	}
	var meta *service.AttachmentMeta
	if condition := r.Header.Get("If-None-Match"); condition != "" && condition != "*" {
		writeErr(w, domain.Invalid("attachments support only If-None-Match: *"))
		return
	}
	if r.Header.Get("If-None-Match") == "*" {
		meta, err = s.Svc.PutAttachmentIfAbsent(r.Context(), actorOf(r), spaceID(r), p, data)
	} else {
		meta, err = s.Svc.PutAttachment(r.Context(), actorOf(r), spaceID(r), p, data)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, meta)
}

func (s *Server) handleAttachmentGet(w http.ResponseWriter, r *http.Request) {
	p, err := attachmentPath(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	data, mime, err := s.Svc.ReadAttachment(r.Context(), actorOf(r), spaceID(r), p)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// attachments are inert documents: no scripts, no origin, no referrer
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("Content-Disposition", "inline; filename=\""+safeFilename(path.Base(p))+"\"")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func safeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_', r == ' ', r >= 0x80:
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "attachment"
	}
	return out
}

func (s *Server) handleAttachmentsList(w http.ResponseWriter, r *http.Request) {
	items, err := s.Svc.ListAttachments(r.Context(), actorOf(r), spaceID(r), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"attachments": items})
}
