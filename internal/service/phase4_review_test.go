package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/ports"
	"testing"
)

func TestPhase4ReviewUnresolvedRespectsNodeCap(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sp, err := h.svc.CreateSpace(ctx, h.actor, "Review cap", "shared")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.CreateNote(ctx, h.actor, sp.ID, "A.md", "[[MissingOne]] [[MissingTwo]]", "", false)
	if err != nil {
		t.Fatal(err)
	}
	g, err := h.svc.GraphAnalysis(ctx, h.actor, sp.ID, &ports.GraphQuery{Scope: "vault", Depth: 0, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) > g.Limits.Nodes {
		t.Fatalf("node cap violated: returned=%d limit=%d matched=%d truncated=%v", len(g.Nodes), g.Limits.Nodes, g.Matched.Nodes, g.Truncated)
	}
}

func TestPhase4ReviewZeroDepthIsRootOnly(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sp, err := h.svc.CreateSpace(ctx, h.actor, "Review depth", "shared")
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.svc.CreateNote(ctx, h.actor, sp.ID, "A.md", "[[Missing]]", "", false)
	if err != nil {
		t.Fatal(err)
	}
	g, err := h.svc.GraphAnalysis(ctx, h.actor, sp.ID, &ports.GraphQuery{Scope: "note", ScopeIDs: []string{d.ID}, Depth: 0, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 1 || len(g.Edges) != 0 {
		t.Fatalf("depth zero must be root-only: nodes=%d edges=%d", len(g.Nodes), len(g.Edges))
	}
}

func TestPhase4ReviewDegreeCountsDistinctNeighbors(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sp, err := h.svc.CreateSpace(ctx, h.actor, "Review degree", "shared")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.svc.CreateNote(ctx, h.actor, sp.ID, "B.md", "# B", "", false)
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.svc.CreateNote(ctx, h.actor, sp.ID, "A.md", "[[B]]", "", false)
	if err != nil {
		t.Fatal(err)
	}
	nc, err := h.svc.ReadNote(ctx, h.actor, sp.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.SaveContent(ctx, h.actor, sp.ID, b.ID, nc.Revision, "[[A]]", "")
	if err != nil {
		t.Fatal(err)
	}
	g, err := h.svc.GraphAnalysis(ctx, h.actor, sp.ID, &ports.GraphQuery{Scope: "vault", Depth: 0, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if (n.ID == a.ID || n.ID == b.ID) && n.Importance != 1 {
			t.Fatalf("mutual links have one distinct neighbor, got %d for %s", n.Importance, n.Label)
		}
	}
}
