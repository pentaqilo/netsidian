package service

import (
	"context"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func TestTextFileRoundTripAndIfMatch(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	const path = "diagrams/arch.puml"
	body := []byte("@startuml\nAlice -> Bob: ping\n@enduml\n")

	meta, etag, err := h.svc.WriteTextFile(ctx, h.actor, h.space, path, body, "*")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if meta.Path != path || meta.Size != int64(len(body)) {
		t.Fatalf("meta = %+v", meta)
	}
	data, got, err := h.svc.ReadTextFile(ctx, h.actor, h.space, path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != string(body) || got != etag {
		t.Fatalf("read mismatch: %q etag=%s want %s", data, got, etag)
	}

	if _, _, err := h.svc.WriteTextFile(ctx, h.actor, h.space, path, []byte("stale"), "bogus"); err == nil {
		t.Fatal("stale etag write must fail")
	} else if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeConflict {
		t.Fatalf("stale write err = %v", err)
	}
	_, next, err := h.svc.WriteTextFile(ctx, h.actor, h.space, path, []byte("v2\n"), etag)
	if err != nil {
		t.Fatalf("matched write: %v", err)
	}
	if next == etag {
		t.Fatal("etag must change after save")
	}

	// .puml files list as attachments so the file tree and import dedup see them
	items, err := h.svc.ListAttachments(ctx, h.actor, h.space, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, it := range items {
		if it.Path == path {
			found = true
		}
	}
	if !found {
		t.Fatalf("text file missing from attachment listing: %+v", items)
	}

	if _, _, err := h.svc.ReadTextFile(ctx, h.actor, h.space, "notes/forbidden.md"); err == nil {
		t.Fatal("markdown must not be editable through text files")
	} else if de, ok := err.(*domain.Error); !ok || de.Code != domain.ErrCodeInvalid {
		t.Fatalf("reject err = %v", err)
	}
}
