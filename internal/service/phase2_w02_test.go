// Package-2 W02: vault lifecycle and per-vault workspace state.
//
// Requirements: P2-V03, P2-V04, P2-V05.
package service

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func TestPhase2W02WorkspaceStateIsolationAndRestoration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vaultA, err := h.svc.CreateSpace(ctx, h.actor, "State A", "shared")
	if err != nil {
		t.Fatal(err)
	}
	vaultB, err := h.svc.CreateSpace(ctx, h.actor, "State B", "shared")
	if err != nil {
		t.Fatal(err)
	}
	docA, err := h.svc.CreateNote(ctx, h.actor, vaultA.ID, "Note A.md", "# A\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	docB, err := h.svc.CreateNote(ctx, h.actor, vaultB.ID, "Note B.md", "# B\n", "", false)
	if err != nil {
		t.Fatal(err)
	}

	// Save A's workspace with a foreign (vault-B) tab and active id:
	// the server must strip ids that do not belong to the vault.
	err = h.svc.SetWorkspace(ctx, h.actor, vaultA.ID, &WorkspaceState{
		OpenDocs:        []string{docA.ID, docB.ID},
		ActiveDoc:       docB.ID,
		ExpandedFolders: []string{"Nested/Folder"},
		PaneWidths:      map[string]int{"explorer": 240, "graph": 480},
		GraphScope:      map[string]any{"mode": "document-link", "depth": 1.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := h.svc.GetWorkspace(ctx, h.actor, vaultA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.OpenDocs) != 1 || st.OpenDocs[0] != docA.ID {
		t.Fatalf("foreign-vault tab not stripped: %+v", st.OpenDocs)
	}
	if st.ActiveDoc != "" {
		t.Fatalf("foreign-vault active doc not stripped: %q", st.ActiveDoc)
	}
	if len(st.ExpandedFolders) != 1 || st.ExpandedFolders[0] != "Nested/Folder" {
		t.Fatalf("expansion not restored: %+v", st.ExpandedFolders)
	}
	if st.PaneWidths["explorer"] != 240 || st.PaneWidths["graph"] != 480 {
		t.Fatalf("pane widths not restored: %+v", st.PaneWidths)
	}
	if st.GraphScope["mode"] != "document-link" {
		t.Fatalf("graph scope not restored: %+v", st.GraphScope)
	}

	// Vault B keeps its own (empty) state; state never bleeds across.
	stB, err := h.svc.GetWorkspace(ctx, h.actor, vaultB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stB.OpenDocs) != 0 || stB.ActiveDoc != "" {
		t.Fatalf("vault A state leaked into vault B: %+v", stB)
	}

	// A second member keeps a separate workspace in the same vault.
	member := w01Join(t, h, h.actor, vaultA.ID, "member@w02.phase2.test", domain.RoleEditor)
	if _, err := h.svc.GetWorkspace(ctx, member, vaultA.ID); err != nil {
		t.Fatalf("member may read its own workspace: %v", err)
	}
	stM, err := h.svc.GetWorkspace(ctx, member, vaultA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stM.OpenDocs) != 0 {
		t.Fatalf("owner workspace leaked to member: %+v", stM)
	}
	// Non-members are rejected server-side on both directions.
	viewer := w01Join(t, h, h.actor, vaultB.ID, "outsider@w02.phase2.test", domain.RoleViewer)
	if _, err := h.svc.GetWorkspace(ctx, viewer, vaultA.ID); err == nil {
		t.Fatal("non-member read another vault's workspace")
	}
	if err := h.svc.SetWorkspace(ctx, viewer, vaultA.ID, &WorkspaceState{ActiveDoc: docA.ID}); err == nil {
		t.Fatal("non-member wrote into another vault's workspace")
	}

	// A whole-document preferences replacement (appearance save) must
	// never drop per-vault workspace state.
	if err := h.svc.SetPreferences(ctx, h.actor, map[string]any{"appearance": map[string]any{"theme": "dark"}}); err != nil {
		t.Fatal(err)
	}
	st2, err := h.svc.GetWorkspace(ctx, h.actor, vaultA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.OpenDocs) != 1 || st2.OpenDocs[0] != docA.ID || st2.PaneWidths["graph"] != 480 {
		t.Fatalf("workspace state lost after preferences save: %+v", st2)
	}
}

func TestPhase2W02VaultRenameAuthorization(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vault, err := h.svc.CreateSpace(ctx, h.actor, "Old Name", "shared")
	if err != nil {
		t.Fatal(err)
	}
	editor := w01Join(t, h, h.actor, vault.ID, "editor@w02.phase2.test", domain.RoleEditor)
	viewer := w01Join(t, h, h.actor, vault.ID, "viewer@w02.phase2.test", domain.RoleViewer)
	if _, err := h.svc.RenameSpace(ctx, editor, vault.ID, "Hijacked"); err == nil {
		t.Fatal("editor renamed a vault")
	}
	if _, err := h.svc.RenameSpace(ctx, viewer, vault.ID, "Hijacked"); err == nil {
		t.Fatal("viewer renamed a vault")
	}
	sp, err := h.svc.RenameSpace(ctx, h.actor, vault.ID, "  New Name  ")
	if err != nil || sp.Name != "New Name" || sp.ID != vault.ID {
		t.Fatalf("owner rename failed or changed identity: %v %+v", err, sp)
	}
	if _, err := h.svc.RenameSpace(ctx, h.actor, vault.ID, "   "); err == nil {
		t.Fatal("blank vault name accepted")
	}
	// Members still see the renamed vault with the same id.
	spaces, roles, err := h.svc.Spaces.SpacesForUser(ctx, viewer.UserID)
	if err != nil || len(spaces) != 1 || spaces[0].ID != vault.ID || spaces[0].Name != "New Name" || roles[0] != domain.RoleViewer {
		t.Fatalf("member view broken after rename: %v %+v", err, spaces)
	}
}

func TestPhase2W02VaultExportPortableTree(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vault, err := h.svc.CreateSpace(ctx, h.actor, "Export Vault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"Research", "Archive/Empty"} {
		if err := h.svc.CreateFolder(ctx, h.actor, vault.ID, f); err != nil {
			t.Fatal(err)
		}
	}
	note := "# Alpha\n\nLinks [[Beta]] and [[Missing]].\n"
	if _, err := h.svc.CreateNote(ctx, h.actor, vault.ID, "Research/Alpha.md", note, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.CreateNote(ctx, h.actor, vault.ID, "Beta.md", "# Beta\n", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, vault.ID, "img/photo.png", fixturePNG); err != nil {
		t.Fatal(err)
	}

	// Viewer may export (ActionExport); non-member may not.
	viewer := w01Join(t, h, h.actor, vault.ID, "peek@w02.phase2.test", domain.RoleViewer)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	sum, err := h.svc.ExportVault(ctx, viewer, vault.ID, zw)
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if sum.Documents != 2 || sum.Attachments != 1 || sum.Folders < 2 {
		t.Fatalf("summary mismatch: %+v", sum)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	dirs := map[string]bool{}
	for _, f := range zr.File {
		if f.Name[len(f.Name)-1] == '/' {
			dirs[f.Name] = true
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if _, err := b.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
		files[f.Name] = b.String()
	}
	if files["Research/Alpha.md"] != note {
		t.Fatalf("note bytes not vault-relative/identical: %q", files["Research/Alpha.md"])
	}
	if files["Beta.md"] != "# Beta\n" {
		t.Fatalf("root note wrong: %q", files["Beta.md"])
	}
	if !dirs["Archive/Empty/"] || !dirs["Research/"] {
		t.Fatalf("empty folders missing: %v", dirs)
	}
	if len(files["img/photo.png"]) != len(fixturePNG) {
		t.Fatal("attachment bytes altered in export")
	}
	if !bytes.Contains([]byte(files["img/photo.png"]), []byte("PNG")) {
		t.Fatal("attachment is not the original PNG bytes")
	}
	mani, ok := files["netsidian/vault.json"]
	if !ok || !bytes.Contains([]byte(mani), []byte(vault.ID)) {
		t.Fatalf("separable metadata manifest missing: %q", mani)
	}
	// The unresolved [[Missing]] target must not appear anywhere.
	for name := range files {
		if name == "Missing.md" {
			t.Fatal("unresolved link materialised in export")
		}
	}
	outsider := w01User(t, h, "nobody@w02.phase2.test")
	var sink bytes.Buffer
	if _, err := h.svc.ExportVault(ctx, outsider, vault.ID, zip.NewWriter(&sink)); err == nil {
		t.Fatal("non-member exported the vault")
	}
}
