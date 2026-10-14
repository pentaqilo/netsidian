// Package-2 W01: vault identity baseline.
//
// Requirements: P2-V01, P2-V02, contracts C01/C08.
// The vaultId is the existing space.id verbatim; "Vault" is terminology
// only. This test seeds the isolated C08 fixture corpus through the
// real service layer and asserts identity preservation: stable IDs,
// per-vault path resolution, byte-identical content/attachments,
// resolved-vs-unresolved link state, empty folders, and viewer/editor
// isolation across two vaults. When PHASE2_FIXTURE_OUT names a JSON
// file the run also writes a manifest of IDs/paths/revisions/SHA-256
// byte digests for later regression packages to re-assert against.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// minimal 1x1 transparent PNG and a minimal one-page PDF, byte-exact
var fixturePNG, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGMAAABABQABTUDWYAAAAABJRU5ErkJggg==")

var fixturePDF = []byte("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
	"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 24 24]>>endobj\n" +
	"trailer<</Root 1 0 R>>\n%%EOF\n")

func fixtureSHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type w01Doc struct {
	Vault    string `json:"vault"`
	ID       string `json:"id"`
	Path     string `json:"path"`
	Revision string `json:"revision"`
	SHA256   string `json:"sha256"`
}

type w01Manifest struct {
	VaultA      string   `json:"vaultA"`
	VaultB      string   `json:"vaultB"`
	Documents   []w01Doc `json:"documents"`
	FoldersA    []string `json:"foldersA"`
	Attachments []w01Doc `json:"attachments"`
}

func w01User(t *testing.T, h *harness, email string) *Actor {
	t.Helper()
	hpw, err := hashPassword("fixture-secret-0001")
	if err != nil {
		t.Fatal(err)
	}
	u := &domain.User{ID: domain.NewID("usr"), Email: email, DisplayName: email, PasswordHash: hpw}
	if err := h.svc.Users.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return &Actor{UserID: u.ID}
}

// w01Join invites email with role into space and returns the joined
// actor (user is created first so the invitation has a real account).
func w01Join(t *testing.T, h *harness, owner *Actor, space, email string, role domain.Role) *Actor {
	t.Helper()
	ctx := context.Background()
	a := w01User(t, h, email)
	url, _, err := h.svc.Invite(ctx, owner, space, email, role)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(url, "/invite?token=")
	if _, err := h.svc.AcceptInvitation(ctx, a, token); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPhase2W01FixtureVaultsAndIdentity(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// ---- seed two isolated vaults through the real services ----
	vaultA, err := h.svc.CreateSpace(ctx, h.actor, "Alpha Vault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	vaultB, err := h.svc.CreateSpace(ctx, h.actor, "Beta Vault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	// C01: vaultId is the space.id verbatim, no migration, no remap.
	if !strings.HasPrefix(vaultA.ID, "spc_") || !strings.HasPrefix(vaultB.ID, "spc_") {
		t.Fatalf("vault ids are not space ids: %s %s", vaultA.ID, vaultB.ID)
	}

	for _, f := range []string{"Research", "Research/Deep", "Archive", "Archive/Empty"} {
		if err := h.svc.CreateFolder(ctx, h.actor, vaultA.ID, f); err != nil {
			t.Fatal(err)
		}
	}

	// deterministic corpus: explicit links, one bridge note, one
	// unresolved link, one orphan, a duplicate title/path in vault B,
	// and a code-literal note whose fenced content must stay byte-exact
	// (guard for later live-preview packages).
	alpha := "# Alpha\n\nBridge to [[Beta]].\n"
	beta := "# Beta\n\nAlso [[Research/Deep/Gamma]] and [[Missing Note]].\n"
	gamma := "# Gamma\n\nDeep note, links [[Alpha]] again.\n"
	orphan := "# Orphan\n\nNo links at all.\n"
	codelit := "# Code\n\n```md\n[[not-a-link]] and `inline [x](y)` stay literal\n```\n"
	dupB := "# Alpha\n\nSame title and path, different vault.\n"

	type seed struct{ vault, path, body string }
	seeds := []seed{
		{vaultA.ID, "Research/Alpha.md", alpha},
		{vaultA.ID, "Research/Beta.md", beta},
		{vaultA.ID, "Research/Deep/Gamma.md", gamma},
		{vaultA.ID, "Orphan.md", orphan},
		{vaultA.ID, "Code.md", codelit},
		{vaultB.ID, "Research/Alpha.md", dupB},
	}
	ids := map[string]string{} // vault+"/"+path -> doc id
	for _, s := range seeds {
		m, err := h.svc.CreateNote(ctx, h.actor, s.vault, s.path, s.body, "", false)
		if err != nil {
			t.Fatalf("seed %s: %v", s.path, err)
		}
		ids[s.vault+"/"+s.path] = m.ID
	}

	if _, err := h.svc.PutAttachment(ctx, h.actor, vaultA.ID, "assets/diagram.png", fixturePNG); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, vaultA.ID, "assets/paper.pdf", fixturePDF); err != nil {
		t.Fatal(err)
	}

	// ---- identity assertions ----

	// 1. Duplicate relative path resolves to distinct documents, and
	//    every read round-trips the fixture bytes verbatim.
	alphaID, dupID := ids[vaultA.ID+"/Research/Alpha.md"], ids[vaultB.ID+"/Research/Alpha.md"]
	if alphaID == dupID {
		t.Fatal("same path in two vaults produced the same document id")
	}
	readA, err := h.svc.ReadNote(ctx, h.actor, vaultA.ID, alphaID)
	if err != nil || readA.Markdown != alpha || readA.Revision == "" {
		t.Fatalf("vault A read mismatch: %v %+v", err, readA)
	}
	readB, err := h.svc.ReadNote(ctx, h.actor, vaultB.ID, dupID)
	if err != nil || readB.Markdown != dupB {
		t.Fatalf("vault B read mismatch: %v %+v", err, readB)
	}
	if c, err := h.svc.ReadNote(ctx, h.actor, vaultA.ID, ids[vaultA.ID+"/Code.md"]); err != nil || c.Markdown != codelit {
		t.Fatalf("code-literal fixture not byte-identical: %v", err)
	}

	// 2. IDs and revisions are stable across repeated listings.
	l1, err := h.svc.ListDocuments(ctx, h.actor, vaultA.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	l2, err := h.svc.ListDocuments(ctx, h.actor, vaultA.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(l1) != 5 || len(l2) != 5 {
		t.Fatalf("expected 5 notes in vault A, got %d/%d", len(l1), len(l2))
	}
	for i := range l1 {
		if l1[i].ID != l2[i].ID || l1[i].CurrentRevision != l2[i].CurrentRevision || l1[i].Path != l2[i].Path {
			t.Fatalf("identity drift between listings: %+v vs %+v", l1[i], l2[i])
		}
	}

	// 3. Empty folders persist.
	folders, err := h.svc.ListFolders(ctx, h.actor, vaultA.ID)
	if err != nil {
		t.Fatal(err)
	}
	hasEmpty := false
	for _, f := range folders {
		if f == "Archive/Empty" {
			hasEmpty = true
		}
	}
	if !hasEmpty {
		t.Fatalf("empty folder missing from listing: %v", folders)
	}

	// 4. Attachments keep their exact bytes and content type.
	for path, want := range map[string][]byte{"assets/diagram.png": fixturePNG, "assets/paper.pdf": fixturePDF} {
		got, ct, err := h.svc.ReadAttachment(ctx, h.actor, vaultA.ID, path)
		if err != nil {
			t.Fatal(err)
		}
		if fixtureSHA(got) != fixtureSHA(want) {
			t.Fatalf("attachment bytes drifted for %s", path)
		}
		if ct == "" {
			t.Fatalf("missing content type for %s", path)
		}
	}

	// 5. Graph edges only exist between resolved links; the unresolved
	//    [[Missing Note]] target must not materialise anywhere, and the
	//    orphan note must carry no edge.
	g, err := h.svc.Graph(ctx, h.actor, vaultA.ID, 1000)
	if err != nil {
		t.Fatal(err)
	}
	nodeIDs := map[string]bool{}
	for _, n := range g.Nodes {
		nodeIDs[n.ID] = true
		if strings.Contains(n.Path, "Missing") {
			t.Fatalf("unresolved link materialised a node: %+v", n)
		}
	}
	edge := func(a, b string) bool {
		for _, e := range g.Edges {
			if (e.Source == a && e.Target == b) || (e.Source == b && e.Target == a) {
				return true
			}
		}
		return false
	}
	if !edge(alphaID, ids[vaultA.ID+"/Research/Beta.md"]) || !edge(ids[vaultA.ID+"/Research/Beta.md"], ids[vaultA.ID+"/Research/Deep/Gamma.md"]) {
		t.Fatalf("resolved links missing from graph edges: %+v", g.Edges)
	}
	for _, e := range g.Edges {
		if e.Source == ids[vaultA.ID+"/Orphan.md"] || e.Target == ids[vaultA.ID+"/Orphan.md"] {
			t.Fatal("orphan note carries a graph edge")
		}
	}
	if _, err := h.svc.ReadNote(ctx, h.actor, vaultA.ID, "doc_missingnote"); err == nil {
		t.Fatal("phantom document readable")
	}

	// 6. Two vaults: a viewer of B cannot read A; an editor of A can
	//    save A but cannot manage members; nothing crosses vaults.
	viewer := w01Join(t, h, h.actor, vaultB.ID, "viewer@fixtures.phase2.test", domain.RoleViewer)
	editor := w01Join(t, h, h.actor, vaultA.ID, "editor@fixtures.phase2.test", domain.RoleEditor)
	if _, err := h.svc.ReadNote(ctx, viewer, vaultA.ID, alphaID); err == nil {
		t.Fatal("vault B viewer read a vault A document")
	}
	if _, err := h.svc.Graph(ctx, viewer, vaultA.ID, 50); err == nil {
		t.Fatal("vault B viewer read the vault A graph")
	}
	saved, err := h.svc.SaveContent(ctx, editor, vaultA.ID, alphaID, readA.Revision, alpha+"\nEdited by editor.\n", "")
	if err != nil {
		t.Fatalf("editor save failed: %v", err)
	}
	after, err := h.svc.ReadNote(ctx, editor, vaultA.ID, alphaID)
	if err != nil || !strings.Contains(after.Markdown, "Edited by editor.") || after.Revision != saved.Revision {
		t.Fatalf("editor save not readable back: %v %+v", err, after)
	}
	if err := h.svc.SetMemberRole(ctx, editor, vaultA.ID, h.actor.UserID, domain.RoleEditor); err == nil {
		t.Fatal("editor managed vault members")
	}

	// ---- optional manifest for later regression packages ----
	if out := os.Getenv("PHASE2_FIXTURE_OUT"); out != "" {
		m := w01Manifest{VaultA: vaultA.ID, VaultB: vaultB.ID}
		for _, s := range seeds {
			id := ids[s.vault+"/"+s.path]
			nc, err := h.svc.ReadNote(ctx, h.actor, s.vault, id)
			if err != nil {
				t.Fatal(err)
			}
			m.Documents = append(m.Documents, w01Doc{Vault: s.vault, ID: id, Path: s.path, Revision: nc.Revision, SHA256: fixtureSHA([]byte(nc.Markdown))})
		}
		m.FoldersA = folders
		for _, a := range []string{"assets/diagram.png", "assets/paper.pdf"} {
			blob, _, err := h.svc.ReadAttachment(ctx, h.actor, vaultA.ID, a)
			if err != nil {
				t.Fatal(err)
			}
			m.Attachments = append(m.Attachments, w01Doc{Vault: vaultA.ID, Path: a, SHA256: fixtureSHA(blob)})
		}
		raw, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
