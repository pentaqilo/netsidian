package service

import (
	"context"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func TestReadNoteEditCapabilityIntersectsTokenScopeAndRole(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	note, err := h.svc.CreateNote(ctx, h.actor, h.space, "Capability.md", "# Capability\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		scopes []string
		want   bool
	}{
		{"read only owner key", []string{domain.ScopeDocumentsRead}, false},
		{"read and write owner key", []string{domain.ScopeDocumentsRead, domain.ScopeDocumentsWrite}, true},
		{"inherited owner key", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := *h.actor
			actor.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: tc.scopes}
			content, err := h.svc.ReadNote(ctx, &actor, h.space, note.ID)
			if err != nil {
				t.Fatal(err)
			}
			if content.CanEdit != tc.want {
				t.Fatalf("canEdit=%v, want %v", content.CanEdit, tc.want)
			}
		})
	}
	viewer := w01Join(t, h, h.actor, h.space, "capability-viewer@x.local", domain.RoleViewer)
	viewer.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{domain.ScopeDocumentsRead, domain.ScopeDocumentsWrite}}
	content, err := h.svc.ReadNote(ctx, viewer, h.space, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if content.CanEdit {
		t.Fatal("write scope must not elevate a viewer's role")
	}
}

func TestAgentWhoamiRecognizesNonDocumentMutations(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, tc := range []struct{ scope, operation string }{
		{domain.ScopeDocumentsDelete, "trash"},
		{domain.ScopeHistoryRestore, "restore"},
		{domain.ScopeAttachmentsWrite, "attachments"},
		{domain.ScopeDocumentsPurge, "purge"},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			actor := *h.actor
			actor.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{tc.scope}}
			who, err := h.svc.AgentWhoami(ctx, &actor)
			if err != nil {
				t.Fatal(err)
			}
			if who.Auth.ReadOnly {
				t.Fatalf("%s authorizes a mutation, must not report readOnly", tc.scope)
			}
			found := false
			for _, v := range who.Vaults {
				for _, op := range v.AllowedOperations {
					if op == tc.operation {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("expected %s in effective operations", tc.operation)
			}
		})
	}
}

func TestAgentWhoamiSpecialReadPrerequisites(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, scope := range []string{domain.ScopeHistoryRead, domain.ScopeEventsRead} {
		actor := *h.actor
		actor.Key = &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{scope}}
		who, err := h.svc.AgentWhoami(ctx, &actor)
		if err != nil {
			t.Fatal(err)
		}
		if len(who.Vaults) != 0 {
			t.Fatalf("%s also needs documents:read; projected unusable operations: %+v", scope, who.Vaults)
		}
	}
}
