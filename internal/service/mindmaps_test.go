package service

import (
	"context"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func TestMindMapCanvasPersistsReferencesAndRejectsStaleEdits(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Mind maps/Launch", "Launch", "map-create")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Nodes) != 0 || len(m.Edges) != 0 {
		t.Fatal("new canvas must be blank")
	}
	m, err = h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, "Idea", 40, 80, "create-idea")
	if err != nil {
		t.Fatal(err)
	}
	owned := m.Nodes[0]
	if !owned.Owned {
		t.Fatal("new node is not an ordinary owned note")
	}
	ext, err := h.svc.CreateNote(ctx, h.actor, h.space, "Research/Evidence.md", "# Evidence\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.AddMindMapReference(ctx, h.actor, h.space, m.Path, ext.ID, 400, 80)
	if err != nil {
		t.Fatal(err)
	}
	if m.Nodes[1].Owned || m.Nodes[1].Path != ext.Path {
		t.Fatal("referencing moved a document")
	}
	before := m.Revision
	cfg := m.Config
	cfg.Nodes[0].X = 120
	cfg.Edges = []MindMapEdge{{ID: "edge-1", From: owned.ID, To: ext.ID, Label: "supported by"}}
	m, err = h.svc.UpdateMindMapConfig(ctx, h.actor, h.space, m.Path, UpdateMindMapConfigRequest{Nodes: cfg.Nodes, Edges: cfg.Edges}, before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.UpdateMindMapConfig(ctx, h.actor, h.space, m.Path, UpdateMindMapConfigRequest{Nodes: []MindMapNode{}}, before); err == nil {
		t.Fatal("stale canvas overwrote layout")
	}
	m, err = h.svc.GetMindMap(ctx, h.actor, h.space, m.Path)
	if err != nil || m.Nodes[0].X != 120 || len(m.Edges) != 1 {
		t.Fatalf("round trip %+v %v", m, err)
	}
	config, err := h.svc.LookupDocument(ctx, h.actor, h.space, m.Path+"/mindmap.md")
	if err != nil {
		t.Fatal(err)
	}
	note, err := h.svc.ReadNote(ctx, h.actor, h.space, config.ID)
	if err != nil || !strings.Contains(note.Markdown, "/Research/Evidence.md") {
		t.Fatal("graph membership link missing", err)
	}
	m, err = h.svc.DeleteMindMapNode(ctx, h.actor, h.space, m.Path, owned.ID, m.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Edges) != 0 {
		t.Fatal("dangling connector")
	}
	if _, err = h.svc.ReadNote(ctx, h.actor, h.space, owned.ID); err != nil {
		t.Fatal("remove from canvas deleted the file")
	}
}

func TestMindMapMovingFilesIntoNodePreservesIdentityAndLinks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Plan", "Plan", "")
	if err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, "Research", 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	node := m.Nodes[0]
	file, err := h.svc.CreateNote(ctx, h.actor, h.space, "Inbox/Source.md", "# Source\n[[/Other]]\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := h.svc.CreateNote(ctx, h.actor, h.space, "Index.md", "[source](Inbox/Source.md#details)", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = h.svc.MoveFileIntoMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, file.ID, "", file.Path); err == nil {
		t.Fatal("unconditional move accepted")
	}
	m, warnings, err := h.svc.MoveFileIntoMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, file.ID, file.CurrentRevision, file.Path)
	if err != nil || len(warnings) > 0 {
		t.Fatal(err, warnings)
	}
	moved, err := h.svc.ReadNote(ctx, h.actor, h.space, file.ID)
	want := m.Path + "/Nodes/" + node.ID + "/Source.md"
	if err != nil || moved.Path != want {
		t.Fatal("identity/path lost", err)
	}
	parent, err := h.svc.ReadNote(ctx, h.actor, h.space, node.ID)
	if err != nil || !strings.Contains(parent.Markdown, want) {
		t.Fatal("node did not link collected file", err)
	}
	index, err := h.svc.ReadNote(ctx, h.actor, h.space, incoming.ID)
	if err != nil || !strings.Contains(index.Markdown, want+"#details") {
		t.Fatal("incoming link not repaired", err)
	}
	if len(m.Nodes) != 1 {
		t.Fatal("collecting a file unexpectedly creates another node")
	}
	if _, _, err = h.svc.MoveFileIntoMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, node.ID, node.Revision, node.Path); err == nil {
		t.Fatal("node collected itself")
	}
	if _, _, err = h.svc.MoveFileIntoMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, file.ID, moved.Revision, file.Path); err == nil {
		t.Fatal("stale path accepted")
	}
}

func TestMindMapPermissionsAndCrossVaultReferences(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Plan", "Plan", "")
	if err != nil {
		t.Fatal(err)
	}
	reader := &Actor{UserID: h.actor.UserID, Key: &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{domain.ScopeDocumentsRead}}}
	if _, err = h.svc.GetMindMap(ctx, reader, h.space, m.Path); err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.CreateMindMapNode(ctx, reader, h.space, m.Path, "Forbidden", 0, 0, ""); err == nil {
		t.Fatal("reader created node")
	}
	if _, err = h.svc.UpdateMindMapConfig(ctx, reader, h.space, m.Path, UpdateMindMapConfigRequest{}, m.Revision); err == nil {
		t.Fatal("reader edited canvas")
	}
	if _, err = h.svc.GetMindMap(ctx, &Actor{UserID: "outsider"}, h.space, m.Path); err == nil {
		t.Fatal("outsider read canvas")
	}
	other, err := h.svc.CreateSpace(ctx, h.actor, "Other", "shared")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := h.svc.CreateNote(ctx, h.actor, other.ID, "Secret.md", "private", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.AddMindMapReference(ctx, h.actor, h.space, m.Path, secret.ID, 0, 0); err == nil {
		t.Fatal("cross-vault reference accepted")
	}
	if _, err = h.svc.UpdateMindMapConfig(ctx, h.actor, h.space, m.Path, UpdateMindMapConfigRequest{Nodes: []MindMapNode{{ID: secret.ID}}}, m.Revision); err == nil {
		t.Fatal("cross-vault node smuggled via config")
	}
}

func TestMindMapValidationHasNoFileSideEffects(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Plan", "Plan", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"../escape", "mindmap.md", ".hidden", "Folder/Title"} {
		if _, err = h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, title, 0, 0, ""); err == nil {
			t.Fatalf("accepted %q", title)
		}
	}
	if _, err = h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, "Out of bounds", 1e7, 0, ""); err == nil {
		t.Fatal("invalid coordinate accepted")
	}
	docs, err := h.svc.ListDocuments(ctx, h.actor, h.space, false)
	if err != nil || len(docs) != 1 {
		t.Fatalf("invalid request created file %+v %v", docs, err)
	}
	m, err = h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, "Idea", 0, 0, "stable-node-key")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, "Idea", 0, 0, "stable-node-key")
	if err != nil || len(replay.Nodes) != 1 {
		t.Fatal("idempotent retry failed", err)
	}
}

func TestMindMapMissingDocumentsAndFolderRename(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Plan", "Plan", "")
	if err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, "Idea", 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	id := m.Nodes[0].ID
	if _, err = h.svc.RenameFolder(ctx, h.actor, h.space, m.Path, "Maps/Renamed"); err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.GetMindMap(ctx, h.actor, h.space, "Maps/Renamed")
	if err != nil || m.Nodes[0].ID != id || !m.Nodes[0].Owned {
		t.Fatal("folder rename lost identity", err)
	}
	if err = h.svc.Trash(ctx, h.actor, h.space, id, m.Nodes[0].Revision); err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.GetMindMap(ctx, h.actor, h.space, m.Path)
	if err != nil || !m.Nodes[0].Missing {
		t.Fatal("missing file not recoverable", err)
	}
	if err = h.svc.Restore(ctx, h.actor, h.space, id); err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.GetMindMap(ctx, h.actor, h.space, m.Path)
	if err != nil || m.Nodes[0].Missing {
		t.Fatal("restored file not resolved", err)
	}
}

func TestMindMapMalformedConfigAndForeignMetadataPreserved(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Plan", "Plan", "")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := h.svc.LookupDocument(ctx, h.actor, h.space, m.Path+"/mindmap.md")
	if err != nil {
		t.Fatal(err)
	}
	text := "---\nnetsidian: mindmap\nversion: 1\ntags: [planning]\ncustom: keep\nnodes: []\nedges: []\n---\nHuman body\n"
	saved, err := h.svc.SaveContent(ctx, h.actor, h.space, doc.ID, doc.CurrentRevision, text, "")
	if err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.UpdateMindMapConfig(ctx, h.actor, h.space, m.Path, UpdateMindMapConfigRequest{Title: "Updated"}, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	note, err := h.svc.ReadNote(ctx, h.actor, h.space, doc.ID)
	if err != nil || !strings.Contains(note.Markdown, "custom: keep") || !strings.Contains(note.Markdown, "Human body") {
		t.Fatal("foreign data lost", err)
	}
	bad := strings.Replace(note.Markdown, "nodes: []", "nodes: wrong", 1)
	if _, err = h.svc.SaveContent(ctx, h.actor, h.space, doc.ID, note.Revision, bad, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.GetMindMap(ctx, h.actor, h.space, m.Path); err == nil {
		t.Fatal("malformed node array silently discarded")
	}
}

func TestMindMapExplicitRootMoveAndReferenceRemoval(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Plan", "Plan", "")
	if err != nil {
		t.Fatal(err)
	}
	note, err := h.svc.CreateNote(ctx, h.actor, h.space, "Inbox/Reference.md", "# Reference\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.AddMindMapReference(ctx, h.actor, h.space, m.Path, note.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.DeleteMindMapNode(ctx, h.actor, h.space, m.Path, note.ID, m.Revision, true); err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.ReadNote(ctx, h.actor, h.space, note.ID); err != nil {
		t.Fatal("reference document trashed", err)
	}
	m, warnings, err := h.svc.MoveDocIntoMindMap(ctx, h.actor, h.space, m.Path, note.ID, note.CurrentRevision, note.Path)
	if err != nil || len(warnings) > 0 {
		t.Fatal(err, warnings)
	}
	if len(m.Nodes) != 1 || !m.Nodes[0].Owned || m.Nodes[0].Path != "Maps/Plan/Reference.md" {
		t.Fatalf("move lost filename or ownership %+v", m.Nodes)
	}
	if _, err = h.svc.DeleteMindMapNode(ctx, h.actor, h.space, m.Path, note.ID, m.Revision, true); err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.ReadNote(ctx, h.actor, h.space, note.ID); err == nil {
		t.Fatal("owned document was not trashed")
	}
}

func TestMindMapLinkFileDoesNotMoveAndProtectsNodeDraft(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Links", "Links", "")
	if err != nil {
		t.Fatal(err)
	}
	m, err = h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, "Idea", 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	node := m.Nodes[0]
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Research/Source.md", "# Source\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.LinkFileToMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, source.ID, "", node.Revision)
	if err != nil {
		t.Fatal(err)
	}
	after, err := h.svc.ReadNote(ctx, h.actor, h.space, source.ID)
	if err != nil || after.Path != source.Path || after.Markdown != "# Source\n" {
		t.Fatalf("source changed: %+v %v", after, err)
	}
	parent, err := h.svc.ReadNote(ctx, h.actor, h.space, node.ID)
	if err != nil || !strings.Contains(parent.Markdown, "](/Research/Source.md)") {
		t.Fatal("missing Markdown link", err)
	}
	links, err := h.svc.Backlinks(ctx, h.actor, h.space, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, link := range links {
		if link.DocumentID == node.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("node link missing from backlinks")
	}
	if _, err = h.svc.LinkFileToMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, source.ID, "", node.Revision); err == nil {
		t.Fatal("stale node revision accepted")
	}
	if _, err = h.svc.LinkFileToMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, source.ID, "", parent.Revision); err != nil {
		t.Fatal(err)
	}
	again, _ := h.svc.ReadNote(ctx, h.actor, h.space, node.ID)
	if again.Markdown != parent.Markdown {
		t.Fatal("duplicate link appended")
	}
	if _, err = h.svc.LinkFileToMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, source.ID, "", ""); err == nil {
		t.Fatal("node revision not required")
	}
	if _, err = h.svc.LinkFileToMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, node.ID, "", parent.Revision); err == nil {
		t.Fatal("self link accepted")
	}
	_, err = h.svc.PutAttachment(ctx, h.actor, h.space, "Assets/Sketch.pdf", []byte("%PDF-1.4\nfixture\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.LinkFileToMindMapNode(ctx, h.actor, h.space, m.Path, node.ID, "", "Assets/Sketch.pdf", parent.Revision); err != nil {
		t.Fatal(err)
	}
	if _, _, err = h.svc.ReadAttachment(ctx, h.actor, h.space, "Assets/Sketch.pdf"); err != nil {
		t.Fatal("attachment moved", err)
	}
}

func TestMindMapConnectorSidesAndSizeRoundTrip(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m, err := h.svc.CreateMindMap(ctx, h.actor, h.space, "Maps/Sides", "Sides", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Top", "Bottom"} {
		m, err = h.svc.CreateMindMapNode(ctx, h.actor, h.space, m.Path, name, 0, 0, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	nodes := append([]MindMapNode{}, m.Config.Nodes...)
	nodes[0].W = 320
	nodes[0].H = 240
	edges := []MindMapEdge{{ID: "vertical", From: nodes[0].ID, To: nodes[1].ID, FromSide: "bottom", ToSide: "top"}}
	m, err = h.svc.UpdateMindMapConfig(ctx, h.actor, h.space, m.Path, UpdateMindMapConfigRequest{Nodes: nodes, Edges: edges}, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := h.svc.GetMindMap(ctx, h.actor, h.space, m.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Edges[0].FromSide != "bottom" || loaded.Config.Edges[0].ToSide != "top" || loaded.Config.Nodes[0].H != 240 {
		t.Fatalf("layout lost: %+v", loaded.Config)
	}
	edges[0].FromSide = "invalid"
	if _, err = h.svc.UpdateMindMapConfig(ctx, h.actor, h.space, m.Path, UpdateMindMapConfigRequest{Edges: edges}, m.Revision); err == nil {
		t.Fatal("invalid side accepted")
	}
}
