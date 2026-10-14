package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/ports"
	"testing"
)

func TestWorkspaceForeignHistoryDenied(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	other, err := h.svc.CreateSpace(ctx, h.actor, "Other", "shared")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := h.svc.CreateNote(ctx, h.actor, other.ID, "private.md", "private history", "", false)
	if err != nil {
		t.Fatal(err)
	}
	limited := *h.actor
	limited.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{"documents:read", "history:read"}}
	if _, err = h.svc.Revisions(ctx, &limited, h.space, doc.ID); err == nil {
		t.Fatal("foreign history metadata exposed")
	}
	if _, err = h.svc.RevisionContent(ctx, &limited, h.space, doc.ID, doc.CurrentRevision); err == nil {
		t.Fatal("foreign history bytes exposed")
	}
}
func TestWorkspaceKeyCannotMintKey(t *testing.T) {
	h := newHarness(t)
	a := *h.actor
	a.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{"documents:read"}}
	if _, _, err := h.svc.CreateKey(context.Background(), &a, "escalation", nil, nil, 0); err == nil {
		t.Fatal("restricted key minted another key")
	}
}
func TestWorkspaceEmptyFoldersPersist(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.svc.CreateFolder(ctx, h.actor, h.space, "Ideas/Unwritten"); err != nil {
		t.Fatal(err)
	}
	paths, err := h.svc.ListFolders(ctx, h.actor, h.space)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range paths {
		if p == "Ideas/Unwritten" {
			found = true
		}
	}
	if !found {
		t.Fatalf("persistent folder missing: %v", paths)
	}
}
