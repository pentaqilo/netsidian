package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pentaqilo/netsidian/internal/ports"
)

func w04Fixture(t *testing.T, h *harness) map[string]string {
	t.Helper()
	ctx := context.Background()
	specs := map[string]string{
		"A.md":                    "# A\n\nlinks to [[B]]\n",
		"Research/B.md":           "# B\n\nlinks to [[C]] and [[Missing Note]]\n",
		"Research/Nested/C.md":    "# C\n\nterminal\n",
		"Research-old/D.md":       "# D\n\nisolated in an old folder\n",
		"E.md":                    "# E\n\n```[[NotALink]]```\n\n`[[AlsoNot]]` stays literal\n",
		"Journal/2026/Sep/Log.md": "# Log\n\ntoday links [[B]]\n",
	}
	ids := map[string]string{}
	for p, md := range specs {
		meta, err := h.svc.CreateNote(ctx, h.actor, h.space, p, md, "", false)
		if err != nil {
			t.Fatalf("create %s: %v", p, err)
		}
		ids[p] = meta.ID
	}
	return ids
}

func analysisNodes(g *ports.AnalysisGraph) map[string]ports.AnalysisNode {
	out := map[string]ports.AnalysisNode{}
	for _, n := range g.Nodes {
		out[n.ID] = n
	}
	return out
}

func TestPhase2W04AnalysisScopesEdgesAndCode(t *testing.T) {
	h := newHarness(t)
	ids := w04Fixture(t, h)
	ctx := context.Background()
	q := func(f func(*ports.GraphQuery)) *ports.GraphQuery {
		gq := &ports.GraphQuery{Scope: "vault", Direction: "both", Depth: -1}
		f(gq)
		return gq
	}

	vault, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, q(func(*ports.GraphQuery) {}))
	if err != nil {
		t.Fatal(err)
	}
	nodes := analysisNodes(vault)
	if len(nodes) != 7 { // six documents + one unresolved target
		t.Fatalf("vault nodes=%d (%v)", len(nodes), vault.Nodes)
	}
	if vault.Version != 1 || vault.Status != "ready" {
		t.Fatalf("envelope %+v", vault)
	}
	if vault.Matched.Nodes != 6 {
		t.Fatalf("matched docs=%d", vault.Matched.Nodes)
	}
	resolved := map[string]bool{}
	for _, e := range vault.Edges {
		if strings.HasPrefix(e.Target, "u:") {
			continue
		}
		resolved[e.Source+">"+e.Target] = true
	}
	want := map[string]bool{ids["A.md"] + ">" + ids["Research/B.md"]: true, ids["Research/B.md"] + ">" + ids["Research/Nested/C.md"]: true, ids["Journal/2026/Sep/Log.md"] + ">" + ids["Research/B.md"]: true}
	if len(resolved) != len(want) {
		t.Fatalf("resolved edges=%v want=%v", resolved, want)
	}
	for k := range want {
		if !resolved[k] {
			t.Fatalf("missing edge %s in %v", k, resolved)
		}
	}
	for _, n := range vault.Nodes {
		if strings.Contains(n.Label, "NotALink") || strings.Contains(n.Label, "AlsoNot") {
			t.Fatalf("code-literal link leaked into graph: %s", n.Label)
		}
	}
	// evidence carries the real source revision and is not stale
	for _, e := range vault.Edges {
		if len(e.Evidence) == 0 || e.Evidence[0].Revision == "" || e.Evidence[0].Stale {
			t.Fatalf("edge evidence %+v", e)
		}
	}

	noteOut, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, q(func(gq *ports.GraphQuery) {
		gq.Scope, gq.ScopeIDs, gq.Direction, gq.Depth = "note", []string{ids["Research/B.md"]}, "out", 1
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := analysisNodes(noteOut)[ids["A.md"]]; ok {
		t.Fatal("direction out included predecessor A")
	}
	if _, ok := analysisNodes(noteOut)[ids["Research/Nested/C.md"]]; !ok {
		t.Fatal("direction out missed C")
	}
	if noteOut.Query.Depth != 1 || noteOut.Query.Mode != "document" {
		t.Fatalf("effective query %+v", noteOut.Query)
	}

	noteIn, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, q(func(gq *ports.GraphQuery) {
		gq.Scope, gq.ScopeIDs, gq.Direction, gq.Depth = "note", []string{ids["Research/B.md"]}, "in", 1
	}))
	if err != nil {
		t.Fatal(err)
	}
	nin := analysisNodes(noteIn)
	if _, ok := nin[ids["A.md"]]; !ok {
		t.Fatal("direction in missed A")
	}
	for _, n := range noteIn.Nodes {
		if n.Kind == "unresolved" {
			t.Fatal("direction in attached an unresolved target")
		}
	}

	zero, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, q(func(gq *ports.GraphQuery) {
		gq.Scope, gq.ScopeIDs, gq.Depth = "note", []string{ids["Research-old/D.md"]}, 0
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(zero.Nodes) != 1 || zero.Nodes[0].ID != ids["Research-old/D.md"] || len(zero.Edges) != 0 {
		t.Fatalf("explicit depth 0 must return only the root: %+v", zero)
	}

	folder, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, q(func(gq *ports.GraphQuery) {
		gq.Scope, gq.ScopePath = "folder", "Research"
	}))
	if err != nil {
		t.Fatal(err)
	}
	fn := analysisNodes(folder)
	if len(fn) != 3 { // B, C, and B's unresolved target; not D from Research-old
		t.Fatalf("folder nodes=%d", len(fn))
	}
	if _, ok := fn[ids["Research-old/D.md"]]; ok {
		t.Fatal("folder prefix bled into Research-old")
	}

	sel, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, q(func(gq *ports.GraphQuery) {
		gq.Scope, gq.ScopeIDs = "selection", []string{ids["A.md"], ids["Research/Nested/C.md"]}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(sel.Nodes) != 2 || len(sel.Edges) != 0 || sel.Matched.Edges != 0 {
		t.Fatalf("selection without depth=%+v", sel)
	}
	selDeep, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, q(func(gq *ports.GraphQuery) {
		gq.Scope, gq.ScopeIDs, gq.Depth = "selection", []string{ids["A.md"], ids["Research/Nested/C.md"]}, 1
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := analysisNodes(selDeep)[ids["Research/B.md"]]; !ok {
		t.Fatal("selection depth 1 did not reach the bridge note B")
	}
}

func TestPhase2W04AnalysisValidation(t *testing.T) {
	h := newHarness(t)
	ids := w04Fixture(t, h)
	ctx := context.Background()
	bad := []*ports.GraphQuery{
		{Scope: "wat", Depth: -1},
		{Scope: "note", Depth: -1},
		{Scope: "note", ScopeIDs: []string{ids["A.md"], ids["E.md"]}, Depth: -1},
		{Scope: "selection", Depth: -1},
		{Scope: "folder", Depth: -1},
		{Scope: "vault", Depth: 4},
		{Scope: "vault", Depth: -2},
		{Scope: "vault", Direction: "sideways", Depth: -1},
		{Scope: "vault", Mode: "concepts", Depth: -1},
		{Scope: "vault", ScopeIDs: []string{"doc_foreign"}, Depth: -1},
		{Scope: "vault", Limit: -5, Depth: -1},
	}
	for i, q := range bad {
		if _, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, q); err == nil {
			t.Fatalf("query %d (%+v) unexpectedly accepted", i, q)
		}
	}
}

func TestPhase2W04ScopedDiscoveryBeyondCap(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// 1,150 plain notes plus a hub linking the final one: the hub scope must
	// match everything and only then apply render caps (P2-G/G07 parity).
	const fillers = 1150
	for i := 0; i < fillers; i++ {
		if _, err := h.svc.CreateNote(ctx, h.actor, h.space, fmt.Sprintf("Notes/N%04d.md", i), "# filler\n", "", false); err != nil {
			t.Fatal(err)
		}
	}
	target, err := h.svc.CreateNote(ctx, h.actor, h.space, "Notes/TARGET.md", "# target\n\nlate note\n", "", false)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for i := 0; i < fillers; i++ {
		fmt.Fprintf(&sb, "[[Notes/N%04d]]\n", i)
	}
	sb.WriteString("[[Notes/TARGET]]\n")
	hub, err := h.svc.CreateNote(ctx, h.actor, h.space, "Hub.md", sb.String(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	g, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, &ports.GraphQuery{Scope: "note", ScopeIDs: []string{hub.ID}, Direction: "out", Depth: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if g.Matched.Nodes != fillers+2 {
		t.Fatalf("matched=%d want %d", g.Matched.Nodes, fillers+2)
	}
	if len(g.Nodes) != 10 || !g.Truncated || g.Reason != "node-limit" { // the node cap bounds the full response, seed included
		t.Fatalf("returned=%d truncated=%v reason=%q", len(g.Nodes), g.Truncated, g.Reason)
	}
	if len(g.Edges) != len(g.Nodes)-1 {
		t.Fatalf("inconsistent induced edges=%d nodes=%d", len(g.Edges), len(g.Nodes))
	}
	// a note-scoped query for the target works even though it sits beyond the first 1,000
	tg, err := h.svc.GraphAnalysis(ctx, h.actor, h.space, &ports.GraphQuery{Scope: "note", ScopeIDs: []string{target.ID}, Direction: "in", Depth: 1, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range tg.Nodes {
		if n.ID == hub.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("hub beyond the first 1,000 documents was not discovered")
	}
}

func TestGraphSearchAndFolderFilterBeforeNodeCap(t *testing.T) {
	h := newHarness(t)
	ids := w04Fixture(t, h)
	ctx := context.Background()
	all, e := h.svc.GraphAnalysis(ctx, h.actor, h.space, &ports.GraphQuery{Scope: "vault", Depth: 0, Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if !all.Truncated || len(all.Nodes) != 1 {
		t.Fatalf("fixture did not truncate: %+v", all)
	}
	target := "Research/Nested/C.md"
	if all.Nodes[0].ID == ids[target] {
		target = "Research-old/D.md"
	}
	filtered, e := h.svc.GraphAnalysis(ctx, h.actor, h.space, &ports.GraphQuery{Scope: "vault", Query: target, Depth: 0, Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if len(filtered.Nodes) != 1 || filtered.Nodes[0].ID != ids[target] || filtered.Truncated {
		t.Fatalf("search applied after cap: %+v", filtered)
	}
	scoped, e := h.svc.GraphAnalysis(ctx, h.actor, h.space, &ports.GraphQuery{Scope: "folder", ScopePath: "Research", Query: "Nested", Depth: 0})
	if e != nil {
		t.Fatal(e)
	}
	if len(scoped.Nodes) != 1 || scoped.Nodes[0].ID != ids["Research/Nested/C.md"] {
		t.Fatalf("folder/query scope: %+v", scoped)
	}
}
