package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pentaqilo/netsidian/internal/repository/sqlite"
	"github.com/pentaqilo/netsidian/internal/service"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

// Exercise real application persistence and read APIs, rather than only seeded
// SQL rows. The original instance is moved away before reading restored history.
func TestApplicationRestoreWorksWithoutSource(t *testing.T) {
	f := newFixture(t, t.TempDir())
	ctx := context.Background()
	if _, err := f.db.SQL.Exec(`DELETE FROM sessions; DELETE FROM api_keys; DELETE FROM users;`); err != nil {
		t.Fatal(err)
	}
	content, err := local.NewContent(f.storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	state, err := local.NewState(filepath.Join(f.stateDir, "journal", "ops"), filepath.Join(f.stateDir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewServices(service.Config{AuthMode: "local", MaxNoteBytes: 1 << 20, MaxAttachBytes: 1 << 20}, f.db, f.db, f.db, f.db, f.db, f.db, f.db, content, state)
	u, err := svc.SetupAccount(ctx, "roundtrip@example.test", "roundtrip-password", "Operator")
	if err != nil {
		t.Fatal(err)
	}
	a := &service.Actor{UserID: u.ID, IsOperator: true}
	sp, err := svc.CreateSpace(ctx, a, "Vault", "personal")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := svc.CreateNote(ctx, a, sp.ID, "note.md", "# Original\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveContent(ctx, a, sp.ID, doc.ID, doc.CurrentRevision, "# Updated\n", ""); err != nil {
		t.Fatal(err)
	}
	attachment := []byte("%PDF-1.4\nfixture attachment")
	if _, err := svc.PutAttachment(ctx, a, sp.ID, "files/example.pdf", attachment); err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := svc.CreateKey(ctx, a, "backup-test", []string{sp.ID}, []string{"documents:read"}, 30)
	if err != nil {
		t.Fatal(err)
	}
	archive := f.backup(t, Options{})
	if err := f.db.SQL.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.stateDir, f.stateDir+"-offline"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.storageRoot, f.storageRoot+"-offline"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored")
	report, err := Restore(ctx, RestoreOptions{Archive: archive, TargetStateDir: target})
	if err != nil {
		t.Fatal(err)
	}
	if report.ContentVerified != 1 || report.HistoryVerified != 1 || report.Attachments != 1 {
		t.Fatalf("report: %+v", report)
	}
	db, err := sqlite.Open(filepath.Join(target, "database", "netsidian.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	restoredContent, err := local.NewContent(filepath.Join(target, "content"))
	if err != nil {
		t.Fatal(err)
	}
	restoredState, err := local.NewState(filepath.Join(target, "journal", "ops"), filepath.Join(target, "history"))
	if err != nil {
		t.Fatal(err)
	}
	restored := service.NewServices(svc.Cfg, db, db, db, db, db, db, db, restoredContent, restoredState)
	if _, _, _, err := restored.LoginSession(ctx, u.Email, "roundtrip-password"); err != nil {
		t.Fatal("restored login", err)
	}
	if id, ok, err := restored.ResolveSession(ctx, token); err != nil || !ok || id != u.ID {
		t.Fatal("restored session", err)
	}
	if _, _, err := restored.AuthenticateKey(ctx, key); err != nil {
		t.Fatal("restored PAT", err)
	}
	current, err := restored.ReadNote(ctx, a, sp.ID, doc.ID)
	if err != nil || current.Markdown != "# Updated\n" {
		t.Fatalf("restored note: %+v %v", current, err)
	}
	old, err := restored.RevisionContent(ctx, a, sp.ID, doc.ID, doc.CurrentRevision)
	if err != nil || old != "# Original\n" {
		t.Fatalf("restored history: %q %v", old, err)
	}
	b, err := os.ReadFile(filepath.Join(target, "content", "spaces", sp.ID, "files", "example.pdf"))
	if err != nil || string(b) != string(attachment) {
		t.Fatal("restored attachment", err)
	}
}
