package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"strings"
	"testing"
)

func TestIndependentBoardTitlesCannotRelocateTasks(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Review", nil, "Review", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"../done/Escape", "../../Outside", "Nested/Task", "."} {
		if card, err := h.svc.CreateTask(ctx, h.actor, h.space, "Tasks/Review", "todo", title, "original", ""); err == nil {
			t.Fatalf("unsafe title %q created %+v", title, card)
		}
	}
}

func TestIndependentBoardMovesPreserveIdentityLinksAndMemoTags(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Review", nil, "Review", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.CreateNote(ctx, h.actor, h.space, "Tasks/Review/todo/B.md", "# B\n", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PutAttachment(ctx, h.actor, h.space, "Tasks/Review/assets/diagram.mmd", []byte("graph TD; A-->B")); err != nil {
		t.Fatal(err)
	}
	a, err := h.svc.CreateTask(ctx, h.actor, h.space, "Tasks/Review", "todo", "A", "---\ntags: [memo]\n---\n# A\n[[B]] [Diagram](../assets/diagram.mmd)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := h.svc.CreateNote(ctx, h.actor, h.space, "Index.md", "[Task](Tasks/Review/todo/A.md#heading)\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := h.svc.MoveTask(ctx, h.actor, h.space, "Tasks/Review", a.ID, "done", a.Revision, a.Path)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != a.ID || moved.Path != "Tasks/Review/done/A.md" || len(moved.Warnings) > 0 {
		t.Fatalf("move: %+v", moved)
	}
	n, err := h.svc.ReadNote(ctx, h.actor, h.space, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tags: [memo]", "[[/Tasks/Review/todo/B]]", "[Diagram](/Tasks/Review/assets/diagram.mmd)"} {
		if !strings.Contains(n.Markdown, want) {
			t.Fatalf("missing %q in %s", want, n.Markdown)
		}
	}
	n, err = h.svc.ReadNote(ctx, h.actor, h.space, incoming.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Markdown, "/Tasks/Review/done/A.md#heading") {
		t.Fatal(n.Markdown)
	}
	if _, err = h.svc.MoveTask(ctx, h.actor, h.space, "Tasks/Review", a.ID, "in-progress", moved.Revision, a.Path); err == nil {
		t.Fatal("stale source path accepted")
	}
	revisions, err := h.svc.Revisions(ctx, h.actor, h.space, a.ID)
	if err != nil || len(revisions) < 2 {
		t.Fatalf("history not retained: %v %v", revisions, err)
	}
}

func TestIndependentBoardTemplateAndPopulatedColumnProtection(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	board, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Review", nil, "Review", "", "board-create")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Review", nil, "Review", "", "board-create")
	if err != nil || replay.Revision != board.Revision {
		t.Fatalf("board replay: %+v %v", replay, err)
	}
	task, err := h.svc.CreateTask(ctx, h.actor, h.space, board.Path, "todo", "A", "", "")
	if err != nil {
		t.Fatal(err)
	}
	board, err = h.svc.ReorderCards(ctx, h.actor, h.space, board.Path, "todo", []string{task.ID, "stale-id"}, board.Revision)
	if err != nil {
		t.Fatal(err)
	}
	before := board.Revision
	if _, err = h.svc.UpdateBoardConfig(ctx, h.actor, h.space, board.Path, UpdateBoardConfigRequest{Columns: board.Config.Columns[1:]}, board.Revision); err == nil {
		t.Fatal("populated column removed")
	}
	board, err = h.svc.GetBoard(ctx, h.actor, h.space, board.Path)
	if err != nil || board.Revision != before || len(board.Columns[0].Cards) != 1 {
		t.Fatal("refused removal altered board")
	}
	copy, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Copy", nil, "Copy", board.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(copy.Config.Order) != 0 || len(copy.Columns[0].Cards) != 0 {
		t.Fatal("template copied memberships/order")
	}
	viewer := &Actor{UserID: "non-member"}
	if _, err := h.svc.CreateTask(ctx, viewer, h.space, board.Path, "todo", "Forbidden", "", ""); err == nil {
		t.Fatal("unauthorized create")
	}
	key := &Actor{UserID: h.actor.UserID, Key: &ports.APIKeyRecord{SpaceIDs: []string{h.space}, Scopes: []string{domain.ScopeDocumentsRead}}}
	if _, err := h.svc.MoveTask(ctx, key, h.space, board.Path, task.ID, "done", task.Revision, task.Path); err == nil {
		t.Fatal("read-only key moved task")
	}
}

func TestIndependentBoardConfigIsNotATask(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Review", nil, "Review", "", ""); err != nil {
		t.Fatal(err)
	}
	d, err := h.svc.LookupDocument(ctx, h.actor, h.space, "Tasks/Review/board.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.MoveTask(ctx, h.actor, h.space, "Tasks/Review", d.ID, "todo", d.CurrentRevision); err == nil {
		t.Fatal("board configuration moved as a task")
	}
}

func TestIndependentBoardEditsPreserveForeignMetadataAndOrder(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Review", nil, "Review", "", ""); err != nil {
		t.Fatal(err)
	}
	a, err := h.svc.CreateTask(ctx, h.actor, h.space, "Tasks/Review", "todo", "A", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.svc.CreateTask(ctx, h.actor, h.space, "Tasks/Review", "todo", "B", "", "")
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.svc.LookupDocument(ctx, h.actor, h.space, "Tasks/Review/board.md")
	if err != nil {
		t.Fatal(err)
	}
	n, err := h.svc.ReadNote(ctx, h.actor, h.space, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(n.Markdown, "---\n", "---\nowner_note: Keep this value\n", 1)
	saved, err := h.svc.SaveContent(ctx, h.actor, h.space, d.ID, n.Revision, text, "")
	if err != nil {
		t.Fatal(err)
	}
	board, err := h.svc.ReorderCards(ctx, h.actor, h.space, "Tasks/Review", "todo", []string{b.ID, a.ID}, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	cols := copyColumns(board.Config.Columns)
	cols[0].Label = "Backlog"
	board, err = h.svc.UpdateBoardConfig(ctx, h.actor, h.space, "Tasks/Review", UpdateBoardConfigRequest{Columns: cols}, board.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Columns[0].Cards) != 2 || board.Columns[0].Cards[0].ID != b.ID {
		t.Fatal("configuration edit lost manual ordering")
	}
	n, err = h.svc.ReadNote(ctx, h.actor, h.space, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Markdown, "owner_note: Keep this value") {
		t.Fatal("board edit destroyed unrelated frontmatter")
	}
}

func TestIndependentBoardFutureVersionCannotBeMutated(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Review", nil, "Review", "", ""); err != nil {
		t.Fatal(err)
	}
	d, err := h.svc.LookupDocument(ctx, h.actor, h.space, "Tasks/Review/board.md")
	if err != nil {
		t.Fatal(err)
	}
	n, err := h.svc.ReadNote(ctx, h.actor, h.space, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.SaveContent(ctx, h.actor, h.space, d.ID, n.Revision, strings.Replace(n.Markdown, "version: 1", "version: 99", 1), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.svc.CreateTask(ctx, h.actor, h.space, "Tasks/Review", "todo", "A", "", ""); err == nil {
		t.Fatal("unsupported board version accepted for mutation")
	}
}

func TestIndependentBoardMovePreservesUnindexedAndExternallyEditedFiles(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.CreateBoard(ctx, h.actor, h.space, "Tasks/Review", nil, "Review", "", ""); err != nil {
		t.Fatal(err)
	}
	task, err := h.svc.CreateTask(ctx, h.actor, h.space, "Tasks/Review", "todo", "A", "original", "")
	if err != nil {
		t.Fatal(err)
	}
	target := "Tasks/Review/done/A.md"
	if err := h.svc.Content.WriteAtomic(h.space, target, []byte("unindexed replacement")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.MoveTask(ctx, h.actor, h.space, "Tasks/Review", task.ID, "done", task.Revision, task.Path); err == nil {
		t.Fatal("unindexed target overwritten")
	}
	bytes, err := h.svc.Content.Read(h.space, target)
	if err != nil || string(bytes) != "unindexed replacement" {
		t.Fatalf("lost target: %s %v", bytes, err)
	}
	if err := h.svc.Content.Delete(h.space, target); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Content.WriteAtomic(h.space, task.Path, []byte("external edit")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.MoveTask(ctx, h.actor, h.space, "Tasks/Review", task.ID, "done", task.Revision, task.Path); err == nil {
		t.Fatal("external edit moved and overwritten")
	}
	bytes, err = h.svc.Content.Read(h.space, task.Path)
	if err != nil || string(bytes) != "external edit" {
		t.Fatalf("lost external source: %s %v", bytes, err)
	}
}
