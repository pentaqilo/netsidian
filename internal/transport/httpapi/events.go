package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// handleEvents streams space events over SSE. It rechecks membership,
// replays from Last-Event-ID and carries only authorized metadata —
// never document bodies.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	if _, err := s.Svc.Require(r.Context(), a, spaceID(r), domain.ActionRead); err != nil {
		writeErr(w, err)
		return
	}
	if a.Key != nil && !hasScopeStr(a.Key.Scopes, domain.ScopeEventsRead) {
		writeErr(w, domain.Forbidden("key lacks scope events:read"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, domain.Unsupported("streaming unsupported"))
		return
	}
	lastSeq := int64(0)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			lastSeq = n
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher.Flush() // Establish the stream even when the vault has no events yet.

	authorized := func() bool {
		fresh, err := s.identify(r)
		if err != nil || fresh == nil {
			return false
		}
		_, err = s.Svc.Require(r.Context(), fresh, spaceID(r), domain.ActionRead)
		return err == nil
	}
	send := func(seq int64, ev any) {
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "id: %d\nevent: change\ndata: %s\n\n", seq, b)
		flusher.Flush()
	}

	// Subscribe before replaying: anything that lands during the replay is
	// buffered and drained by the live loop below, deduplicated by seq, so
	// no window is missed between replay and live delivery.
	hub := s.Svc.Hub(spaceID(r))
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	// replay durable outbox first
	replay, err := s.Svc.Docs.EventsAfter(r.Context(), spaceID(r), lastSeq, 500)
	if err != nil {
		return
	}
	for _, e := range replay {
		if !authorized() {
			return
		}
		send(e.Seq, e)
		lastSeq = e.Seq
	}

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !authorized() {
				return
			}
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e := <-ch:
			if e == nil {
				return
			}
			if !authorized() {
				return
			}
			if e.Seq > lastSeq {
				send(e.Seq, e)
				lastSeq = e.Seq
			}
		}
	}
}

func hasScopeStr(scopes []string, want string) bool {
	for _, sc := range scopes {
		if sc == want {
			return true
		}
	}
	return false
}
