package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func mustErrCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	var de *domain.Error
	if err == nil || !errors.As(err, &de) || de.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func indexNoteOf(t *testing.T, h *harness) (docID, revision, markdown string) {
	t.Helper()
	ctx := context.Background()
	d, err := h.svc.Docs.DocumentByNormKey(ctx, h.space, mustNormKey("agent-memory/index.md"))
	if err != nil || d == nil {
		t.Fatalf("index document missing: %v", err)
	}
	n, err := h.svc.ReadNote(ctx, h.actor, h.space, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	return d.ID, n.Revision, n.Markdown
}

// Legacy remember stays byte-compatible: no v2 fields appear, and
// unknown fields stored by newer writers survive round-trips.
func TestMemoryV2LegacyBytesAndUnknownPreservation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "legacy", Text: "plain claim"}, "k1"); err != nil {
		t.Fatal(err)
	}
	id, rev, md := indexNoteOf(t, h)
	for _, banned := range []string{"schemaVersion", "\"kind\"", "\"status\"", "recordedAt"} {
		if strings.Contains(md, banned) {
			t.Fatalf("legacy remember emitted v2 field %q: %s", banned, md)
		}
	}
	// Inject an unknown field plus v2 metadata as a newer writer would.
	patched := strings.Replace(md, `"text":"plain claim"`, `"text":"plain claim","custom":{"keep":[1,2]},"kind":"fact"`, 1)
	if patched == md {
		t.Fatal("patch failed")
	}
	if _, err := h.svc.SaveContent(ctx, h.actor, h.space, id, rev, patched, ""); err != nil {
		t.Fatal(err)
	}
	// Recall must not lose the entry over the unknown shape.
	res, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "legacy"})
	if err != nil || res.Matched != 1 || res.Entries[0].Kind != "fact" {
		t.Fatalf("recall after patch: %v %+v", err, res)
	}
	// A later legacy update must preserve unknown fields losslessly.
	if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "legacy", Text: "updated claim"}, "k2"); err != nil {
		t.Fatal(err)
	}
	_, _, md2 := indexNoteOf(t, h)
	if !strings.Contains(md2, `"custom":{"keep":[1,2]}`) {
		t.Fatalf("unknown field lost on update: %s", md2)
	}
	if !strings.Contains(md2, `"text":"updated claim"`) {
		t.Fatalf("update not applied: %s", md2)
	}
}

// Entries written by a future server keep their data readable but are
// never rewritten by this version.
func TestMemoryV2FutureSchemaGuard(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "future", Text: "from the future"}, "k1"); err != nil {
		t.Fatal(err)
	}
	id, rev, md := indexNoteOf(t, h)
	patched := strings.Replace(md, `"text":"from the future"`, `"text":"from the future","schemaVersion":99`, 1)
	if _, err := h.svc.SaveContent(ctx, h.actor, h.space, id, rev, patched, ""); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "future"})
	if err != nil || res.Matched != 1 || res.Entries[0].SchemaVersion != 99 {
		t.Fatalf("future entry must stay readable: %v %+v", err, res)
	}
	_, rev2, _ := indexNoteOf(t, h)
	_, err = h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "future", Text: "downgrade attempt"}, "k2")
	mustErrCode(t, err, domain.ErrCodeUnsupported)
	_, err = h.svc.ReviseMemory(ctx, h.actor, h.space, &MemoryReviseInput{Key: "future", Action: "retract", ExpectedRevision: rev2}, "")
	mustErrCode(t, err, domain.ErrCodeUnsupported)
	_, _, md3 := indexNoteOf(t, h)
	if !strings.Contains(md3, `"schemaVersion":99`) {
		t.Fatal("guarded entry was rewritten")
	}
}

func TestMemoryV2RecallFiltersAndContinuationPin(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seeds := []struct {
		key, kind, scopeID, status string
		from, to                   string
	}{
		{"legacy", "", "", "", "", ""},
		{"pref", "preference", "workspace-a", "active", "", ""},
		{"old", "fact", "workspace-a", "superseded", base.Format(time.RFC3339), base.AddDate(0, 1, 0).Format(time.RFC3339)},
		{"gone", "fact", "workspace-b", "retracted", "", ""},
	}
	for i, s := range seeds {
		if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{
			Key: s.key, Text: "text " + s.key, Kind: s.kind, ScopeID: s.scopeID, Status: s.status, ValidFrom: s.from, ValidTo: s.to,
		}, string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	count := func(in *MemoryRecallInput) int {
		r, err := h.svc.RecallMemory(ctx, h.actor, h.space, in)
		if err != nil {
			t.Fatal(err)
		}
		return r.Matched
	}
	if n := count(&MemoryRecallInput{}); n != 4 {
		t.Fatalf("unfiltered must include legacy and historical: %d", n)
	}
	if n := count(&MemoryRecallInput{Status: "active"}); n != 2 {
		t.Fatalf("active must match legacy+active: %d", n)
	}
	if n := count(&MemoryRecallInput{Kind: "fact"}); n != 2 {
		t.Fatalf("kind filter: %d", n)
	}
	if n := count(&MemoryRecallInput{ScopeID: "workspace-a"}); n != 2 {
		t.Fatalf("scopeId filter: %d", n)
	}
	if n := count(&MemoryRecallInput{Status: "all", Kind: "fact"}); n != 2 {
		t.Fatalf("all+kind: %d", n)
	}
	// validOn: the dated entry is only valid inside its interval;
	// entries without an interval are never excluded.
	if n := count(&MemoryRecallInput{ValidOn: base.AddDate(0, 0, 15).Format(time.RFC3339)}); n != 4 {
		t.Fatalf("inside interval must match all: %d", n)
	}
	if n := count(&MemoryRecallInput{ValidOn: base.AddDate(0, 2, 0).Format(time.RFC3339)}); n != 3 {
		t.Fatalf("after interval must drop the dated entry: %d", n)
	}
	// Continuation must pin the full filter configuration.
	page1, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Limit: 1, Kind: "fact"})
	if err != nil || len(page1.Entries) != 1 || !page1.HasMore {
		t.Fatalf("first page: %v %+v", err, page1)
	}
	if _, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Limit: 1, Kind: "fact", Offset: page1.NextOffset, Revision: page1.IndexRevision}); err == nil {
		t.Fatal("offset without queryHash must be rejected")
	}
	if _, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Limit: 1, Kind: "fact", Offset: page1.NextOffset, Revision: page1.IndexRevision, QueryHash: "deadbeefdeadbeef"}); err == nil {
		t.Fatal("mismatched queryHash must be rejected")
	}
	page2, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Limit: 1, Kind: "fact", Offset: page1.NextOffset, Revision: page1.IndexRevision, QueryHash: page1.QueryHash})
	if err != nil || len(page2.Entries) != 1 {
		t.Fatalf("second page: %v %+v", err, page2)
	}
	// Same page with a different filter must not continue the old scan.
	if _, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Limit: 2, Kind: "fact", Status: "all", Offset: page1.NextOffset, Revision: page1.IndexRevision, QueryHash: page1.QueryHash}); err == nil {
		t.Fatal("filter change mid-continuation must be rejected")
	}
}

// Pinned evidence never follows forward silently: recall marks the
// source stale once the document moves on.
func TestMemoryV2SourceRefStaleness(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	src, err := h.svc.CreateNote(ctx, h.actor, h.space, "evidence.md", "v1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{
		Key: "claim", Text: "based on evidence", SourceRefs: []MemorySourceRef{{Target: "evidence.md"}},
	}, "k1"); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "claim"})
	if err != nil || len(res.Entries[0].SourceRefs) != 1 {
		t.Fatalf("recall: %v %+v", err, res)
	}
	sr := res.Entries[0].SourceRefs[0]
	if !sr.Verified || sr.Revision != src.CurrentRevision || sr.Stale {
		t.Fatalf("fresh evidence must be verified and pinned: %+v", sr)
	}
	if _, err := h.svc.SaveContent(ctx, h.actor, h.space, src.ID, src.CurrentRevision, "v2", ""); err != nil {
		t.Fatal(err)
	}
	res, err = h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "claim"})
	if err != nil {
		t.Fatal(err)
	}
	sr = res.Entries[0].SourceRefs[0]
	if !sr.Stale || sr.Revision != src.CurrentRevision {
		t.Fatalf("moved evidence must be reported stale at the pinned revision: %+v", sr)
	}
}

func TestMemoryReviseFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "addr", Text: "wrong address"}, "k1"); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "addr"})
	if err != nil || res.Matched != 1 {
		t.Fatal(err)
	}
	rev := res.IndexRevision
	if _, err := h.svc.ReviseMemory(ctx, h.actor, h.space, &MemoryReviseInput{Key: "addr", Action: "supersede", Text: "right address"}, "k2"); err == nil {
		t.Fatal("expectedRevision is mandatory for revisions")
	} else {
		mustErrCode(t, err, domain.ErrCodePreconditionReq)
	}
	if _, err := h.svc.ReviseMemory(ctx, h.actor, h.space, &MemoryReviseInput{Key: "addr", Action: "supersede", Text: "right address", ExpectedRevision: "rev_stale"}, "k2"); err == nil {
		t.Fatal("stale revision must be rejected")
	} else {
		mustErrCode(t, err, domain.ErrCodeConflict)
	}
	rec, err := h.svc.ReviseMemory(ctx, h.actor, h.space, &MemoryReviseInput{Key: "addr", Action: "supersede", Text: "right address", ExpectedRevision: rev, Reason: "user corrected"}, "k2")
	if err != nil || rec.IndexRevision == rev || rec.SupersededRevision != rev || len(rec.Supersedes) != 1 {
		t.Fatalf("supersede receipt: %v %+v", err, rec)
	}
	res, err = h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "addr"})
	if err != nil || res.Entries[0].Text != "right address" || len(res.Entries[0].Supersedes) != 1 || res.Entries[0].Supersedes[0].IndexRevision != rev {
		t.Fatalf("superseded entry must pin the prior claim revision: %v %+v", err, res.Entries)
	}
	// Idempotent replay returns the committed receipt without a second
	// revision bump.
	again, err := h.svc.ReviseMemory(ctx, h.actor, h.space, &MemoryReviseInput{Key: "addr", Action: "supersede", Text: "right address", ExpectedRevision: rev, Reason: "user corrected"}, "k2")
	if err != nil || again.IndexRevision != rec.IndexRevision {
		t.Fatalf("replay must return the same receipt: %v %+v", err, again)
	}
	// Retraction keeps the entry recallable as history, not deleted.
	res, err = h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "addr"})
	if err != nil {
		t.Fatal(err)
	}
	rec2, err := h.svc.ReviseMemory(ctx, h.actor, h.space, &MemoryReviseInput{Key: "addr", Action: "retract", ExpectedRevision: res.IndexRevision}, "k3")
	if err != nil || rec2.Status != "retracted" {
		t.Fatalf("retract: %v %+v", err, rec2)
	}
	all, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "addr", Status: "all"})
	if err != nil || all.Matched != 1 || all.Entries[0].Status != "retracted" {
		t.Fatalf("retracted entry must stay addressable in history: %v %+v", err, all)
	}
	active, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "addr", Status: "active"})
	if err != nil || active.Matched != 0 {
		t.Fatalf("retracted entry must leave active recall: %v", err)
	}
}

func TestMemoryConsolidateApplyReplayAndResume(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, k := range []struct{ key, text string }{{"dup-1", "first phrasing"}, {"dup-2", "second phrasing"}} {
		if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: k.key, Text: k.text}, "seed-"+k.key); err != nil {
			t.Fatal(err)
		}
	}
	target, err := h.svc.CreateNote(ctx, h.actor, h.space, "notes/topic.md", "old body", "", false)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := h.svc.CreateNote(ctx, h.actor, h.space, "notes/evidence.md", "Independent evidence", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, indexRev, _ := indexNoteOf(t, h)
	plan := func(op string, md string) *MemoryConsolidateInput {
		return &MemoryConsolidateInput{
			OperationID: op, ExpectedRevision: indexRev,
			Sources: []MemoryConsolidateSource{{Key: "dup-1", ExpectedRevision: indexRev}, {DocumentID: evidence.ID, ExpectedRevision: evidence.CurrentRevision}},
			Targets: []MemoryConsolidateTarget{{DocumentID: target.ID, ExpectedRevision: target.CurrentRevision, Markdown: strPtr(md), AdoptManagedSection: true}},
			Entries: []MemoryConsolidateEntry{{Key: "dup-1", Status: "active"}, {Key: "dup-2", Status: "superseded", Supersedes: []string{"dup-2"}}},
		}
	}
	// Dry-run surfaces conflicts without writing.
	dry, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, &MemoryConsolidateInput{
		OperationID: "op-dry", DryRun: true, Sources: []MemoryConsolidateSource{{DocumentID: evidence.ID, ExpectedRevision: evidence.CurrentRevision}},
		Targets: []MemoryConsolidateTarget{{DocumentID: target.ID, ExpectedRevision: "rev_wrong", Markdown: strPtr("x")}},
	})
	if err != nil || dry.Outcome != "conflicts" || len(dry.Warnings) == 0 {
		t.Fatalf("dry-run must report conflicts, not write: %v %+v", err, dry)
	}
	if n, _ := h.svc.ReadNote(ctx, h.actor, h.space, target.ID); n.Markdown != "old body" {
		t.Fatal("dry-run wrote")
	}
	// Real application: managed section rewritten, human body kept,
	// dup-2 marked superseded.
	rec, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan("op-1", "merged body"))
	if err != nil || rec.Outcome != "applied" || !rec.Verified {
		t.Fatalf("apply: %v %+v", err, rec)
	}
	if n, _ := h.svc.ReadNote(ctx, h.actor, h.space, target.ID); !strings.Contains(n.Markdown, "old body") || !strings.Contains(n.Markdown, "merged body") || !strings.Contains(n.Markdown, memoryManagedStart) {
		t.Fatalf("adoption must preserve human content and own only the managed block: %q", n.Markdown)
	}
	res, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "dup-2", Status: "all"})
	if err != nil || res.Entries[0].Status != "superseded" {
		t.Fatalf("entry status not applied: %v %+v", err, res.Entries)
	}
	// Identical replay returns the committed receipt; nothing rewrites.
	again, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan("op-1", "merged body"))
	if err != nil || again.Outcome != "applied" || again.IndexRevision != rec.IndexRevision {
		t.Fatalf("replay: %v %+v", err, again)
	}
	if n, _ := h.svc.ReadNote(ctx, h.actor, h.space, target.ID); n.Revision != rec.Steps[0].Revision {
		t.Fatal("replay repeated a write")
	}
	// Changed plan under the same operationId is rejected, not merged.
	if _, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan("op-1", "different body")); err == nil {
		t.Fatal("changed plan under same operationId must conflict")
	} else {
		mustErrCode(t, err, domain.ErrCodeConflict)
	}
	// Simulated interruption: the target write commits, then the
	// process dies before the step checkpoint. Resume must prove the
	// committed child operation and finish without repeating it.
	h2 := newHarness(t)
	ctx2 := context.Background()
	if _, err := h2.svc.RememberMemory(ctx2, h2.actor, h2.space, &MemoryRememberInput{Key: "e1", Text: "one"}, "s1"); err != nil {
		t.Fatal(err)
	}
	t2, err := h2.svc.CreateNote(ctx2, h2.actor, h2.space, "notes/t.md", "body", "", false)
	if err != nil {
		t.Fatal(err)
	}
	evidence2, err := h2.svc.CreateNote(ctx2, h2.actor, h2.space, "Evidence.md", "Independent evidence", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, indexRev2, _ := indexNoteOf(t, h2)
	plan2 := &MemoryConsolidateInput{
		OperationID: "op-int", ExpectedRevision: indexRev2, Sources: []MemoryConsolidateSource{{DocumentID: evidence2.ID, ExpectedRevision: evidence2.CurrentRevision}},
		Targets: []MemoryConsolidateTarget{{DocumentID: t2.ID, ExpectedRevision: t2.CurrentRevision, Markdown: strPtr("new body"), AdoptManagedSection: true}},
		Entries: []MemoryConsolidateEntry{{Key: "e1", Status: "disputed"}},
	}
	h2.svc.Docs = &guideCrashAfterTarget{DocumentRepo: h2.svc.Docs, crash: true}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("fault was not injected")
			}
		}()
		_, _ = h2.svc.ConsolidateMemory(ctx2, h2.actor, h2.space, plan2)
	}()
	crashed, _ := h2.svc.Docs.Document(ctx2, t2.ID)
	if crashed == nil || crashed.CurrentRevision == t2.CurrentRevision {
		t.Fatal("fault must occur after the target write commits")
	}
	committedTarget := crashed.CurrentRevision
	h2.svc = guideRestart(h2.svc)
	h2.svc.ReconcileOperations(ctx2)
	rec2, err := h2.svc.ConsolidateMemory(ctx2, h2.actor, h2.space, plan2)
	if err != nil || rec2.Outcome != "applied" || rec2.Steps[0].Status != "applied" {
		t.Fatalf("resume: %v %+v", err, rec2)
	}
	if n, _ := h2.svc.ReadNote(ctx2, h2.actor, h2.space, t2.ID); n.Revision != committedTarget {
		t.Fatal("resume repeated the committed target write")
	}
	res2, err := h2.svc.RecallMemory(ctx2, h2.actor, h2.space, &MemoryRecallInput{Key: "e1"})
	if err != nil || res2.Entries[0].Status != "disputed" {
		t.Fatalf("resume did not finish the index step: %v %+v", err, res2.Entries)
	}
}

// A user edit after a partial application is never rolled back: the
// resume reports a conflict instead of overwriting.
func TestMemoryConsolidatePartialPreservesUserEdits(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "e1", Text: "one"}, "s1"); err != nil {
		t.Fatal(err)
	}
	t1, err := h.svc.CreateNote(ctx, h.actor, h.space, "notes/a.md", "user v1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := h.svc.CreateNote(ctx, h.actor, h.space, "Evidence.md", "Independent evidence", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, indexRev, _ := indexNoteOf(t, h)
	plan := &MemoryConsolidateInput{
		OperationID: "op-div", ExpectedRevision: indexRev, Sources: []MemoryConsolidateSource{{DocumentID: evidence.ID, ExpectedRevision: evidence.CurrentRevision}},
		Targets: []MemoryConsolidateTarget{{DocumentID: t1.ID, ExpectedRevision: t1.CurrentRevision, Markdown: strPtr("agent body"), AdoptManagedSection: true}},
		Entries: []MemoryConsolidateEntry{{Key: "e1", Status: "disputed"}},
	}
	// The target write commits, then the process dies before the
	// checkpoint: the journal still shows the step as planned.
	h.svc.Docs = &guideCrashAfterTarget{DocumentRepo: h.svc.Docs, crash: true}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("fault was not injected")
			}
		}()
		_, _ = h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan)
	}()
	// The user edits the applied target before the plan is replayed.
	h.svc = guideRestart(h.svc)
	h.svc.ReconcileOperations(ctx)
	crashed, _ := h.svc.Docs.Document(ctx, t1.ID)
	if _, err := h.svc.SaveContent(ctx, h.actor, h.space, t1.ID, crashed.CurrentRevision, "user v2", ""); err != nil {
		t.Fatal(err)
	}
	rec, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan)
	if err != nil {
		t.Fatalf("partial must return a receipt, not an error: %v", err)
	}
	if rec.Outcome != "partial" || rec.Steps[0].Status != "blocked" || rec.RecoveryNote == "" {
		t.Fatalf("honest partial receipt required: %+v", rec)
	}
	if n, _ := h.svc.ReadNote(ctx, h.actor, h.space, t1.ID); n.Markdown != "user v2" {
		t.Fatal("user edit was rolled back")
	}
}

func strPtr(s string) *string { return &s }

func TestMemoryDerivedStatus(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	base, err := h.svc.CreateNote(ctx, h.actor, h.space, "base.md", "source", "", false)
	if err != nil {
		t.Fatal(err)
	}
	marker := func(id, rev, body string) string {
		return "---\nnetsidian:\n  derived:\n    sources:\n      - documentId: " + id + "\n        revision: " + rev + "\n---\n" + body + "\n"
	}
	d1, err := h.svc.CreateNote(ctx, h.actor, h.space, "derived1.md", marker(base.ID, base.CurrentRevision, "summary"), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.CreateNote(ctx, h.actor, h.space, "derived2.md", marker(d1.ID, d1.CurrentRevision, "digest"), "", false); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.DerivedStatus(ctx, h.actor, h.space)
	if err != nil || len(res.Derived) != 2 {
		t.Fatalf("scan: %v %+v", err, res)
	}
	for _, d := range res.Derived {
		if d.Status != "current" {
			t.Fatalf("fresh derived docs must be current: %+v", d)
		}
	}
	// Moving the base makes derived1 stale, which makes derived2 stale.
	sr, err := h.svc.SaveContent(ctx, h.actor, h.space, base.ID, base.CurrentRevision, "source v2", "")
	if err != nil {
		t.Fatal(err)
	}
	res, err = h.svc.DerivedStatus(ctx, h.actor, h.space)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Derived) != 2 || res.Derived[0].Status != "stale" || !res.Derived[0].Sources[0].Stale {
		t.Fatalf("base move must mark derived stale: %+v", res.Derived)
	}
	// Deleting the source breaks the chain.
	if err := h.svc.Trash(ctx, h.actor, h.space, base.ID, sr.Revision); err != nil {
		t.Fatal(err)
	}
	res, err = h.svc.DerivedStatus(ctx, h.actor, h.space)
	if err != nil {
		t.Fatal(err)
	}
	if res.Derived[0].Status != "broken" || res.Derived[0].Sources[0].Available {
		t.Fatalf("missing source must be broken: %+v", res.Derived[0])
	}
	// A dependency cycle is reported, not looped on.
	h2 := newHarness(t)
	ctx2 := context.Background()
	a, err := h2.svc.CreateNote(ctx2, h2.actor, h2.space, "cycle-a.md", "placeholder", "", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h2.svc.CreateNote(ctx2, h2.actor, h2.space, "cycle-b.md", "placeholder", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h2.svc.SaveContent(ctx2, h2.actor, h2.space, a.ID, a.CurrentRevision, marker(b.ID, b.CurrentRevision, "a"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h2.svc.SaveContent(ctx2, h2.actor, h2.space, b.ID, b.CurrentRevision, marker(a.ID, a.CurrentRevision, "b"), ""); err != nil {
		t.Fatal(err)
	}
	res2, err := h2.svc.DerivedStatus(ctx2, h2.actor, h2.space)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Derived) != 2 || res2.Derived[0].Status != "cyclic" || len(res2.Derived[0].Cycle) == 0 {
		t.Fatalf("cycle must be detected and reported: %+v", res2.Derived)
	}
}
