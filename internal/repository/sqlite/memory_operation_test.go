package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func TestMemoryOperationCommitPublishesGenerationAtomically(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "memory.db"), 5000)
	if err != nil {
		t.Fatal(err)
	}
	defer db.SQL.Close()
	ctx := context.Background()
	now := time.Now()
	if err := db.CreateSpace(ctx, &domain.Space{ID: "spc-memory", Slug: "memory", Name: "Memory", Kind: "shared", StorageBinding: "local-main", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	op := &domain.Operation{ID: "op-memory", SpaceID: "spc-memory", Principal: "user", Kind: domain.OperationKind("memory_consolidate"), Phase: domain.OpPhasePending, CreatedAt: now, UpdatedAt: now}
	if err := db.InsertOperation(ctx, op); err != nil {
		t.Fatal(err)
	}
	before, _ := db.MaxEventSeq(ctx, op.SpaceID)
	if err := db.CommitMemoryOperation(ctx, "other", op.ID, "bad"); err == nil {
		t.Fatal("cross-vault commit accepted")
	}
	unchanged, _ := db.Operation(ctx, op.ID)
	if unchanged.Phase != domain.OpPhasePending {
		t.Fatal("failed commit changed phase")
	}
	if err := db.CommitMemoryOperation(ctx, op.SpaceID, op.ID, "verified-journal"); err != nil {
		t.Fatal(err)
	}
	committed, _ := db.Operation(ctx, op.ID)
	after, _ := db.MaxEventSeq(ctx, op.SpaceID)
	if committed.Phase != domain.OpPhaseCommitted || committed.Error != "verified-journal" || after <= before {
		t.Fatal(committed, before, after)
	}
	if err := db.CommitMemoryOperation(ctx, op.SpaceID, op.ID, "duplicate"); err == nil {
		t.Fatal("duplicate commit must not duplicate invalidation")
	}
	final, _ := db.MaxEventSeq(ctx, op.SpaceID)
	if final != after {
		t.Fatal("duplicate generation event")
	}
}
