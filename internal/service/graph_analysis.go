package service

import (
	"context"
	"sort"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

const (
	analysisDefaultNodes = 1000
	analysisHardNodeCap  = 1000
	analysisHardEdgeCap  = 5000
)

// GraphAnalysis implements the phase-2 analysis contract (C02/C03): it
// authorizes first, scopes and traverses against the complete indexed vault,
// then applies output caps and returns a consistent induced graph.
func (s *Services) GraphAnalysis(ctx context.Context, a *Actor, spaceID string, q *ports.GraphQuery) (*ports.AnalysisGraph, error) {
	if _, err := s.requireReadScope(ctx, a, spaceID, domain.ScopeGraphRead); err != nil {
		return nil, err
	}
	if err := normalizeAnalysisQuery(q); err != nil {
		return nil, err
	}
	docs, err := s.Index.DocsLite(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	links, err := s.Index.LinksForSpace(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*ports.DocLite, len(docs))
	for i := range docs {
		byID[docs[i].ID] = &docs[i]
	}
	if err := validateAnalysisScope(q, byID); err != nil {
		return nil, err
	}

	// Adjacency over the complete authorized index (scope before any cap).
	out := map[string][]string{}
	in := map[string][]string{}
	for _, l := range links {
		if l.State == "resolved" && l.ResolvedID != "" && l.ResolvedID != l.SourceID && byID[l.SourceID] != nil && byID[l.ResolvedID] != nil {
			out[l.SourceID] = append(out[l.SourceID], l.ResolvedID)
			in[l.ResolvedID] = append(in[l.ResolvedID], l.SourceID)
		}
	}

	// Seeds.
	var seeds []string
	switch q.Scope {
	case "vault":
		seeds = make([]string, 0, len(docs))
		for _, d := range docs {
			if q.Query != "" && !strings.Contains(strings.ToLower(d.Path+" "+d.Title+" "+strings.Join(d.Tags, " ")), strings.ToLower(q.Query)) {
				continue
			}
			seeds = append(seeds, d.ID)
		}
	case "folder":
		prefix := strings.Trim(q.ScopePath, "/")
		for _, d := range docs {
			if (d.Path == prefix || strings.HasPrefix(d.Path, prefix+"/")) && (q.Query == "" || strings.Contains(strings.ToLower(d.Path+" "+d.Title+" "+strings.Join(d.Tags, " ")), strings.ToLower(q.Query))) {
				seeds = append(seeds, d.ID)
			}
		}
	default: // note | selection
		seeds = append(seeds, q.ScopeIDs...)
	}
	sort.Strings(seeds)

	// Breadth-first traversal from seeds (depth 0), following direction.
	visited := map[string]bool{}
	frontier := append([]string(nil), seeds...)
	for _, id := range frontier {
		visited[id] = true
	}
	ordered := append([]string(nil), seeds...)
	depth := q.Depth
	if q.Scope == "vault" {
		depth = 0 // whole vault already contains everything
	}
	for hop := 0; hop < depth; hop++ {
		var next []string
		for _, id := range frontier {
			var nbrs []string
			switch q.Direction {
			case "in":
				nbrs = in[id]
			case "out":
				nbrs = out[id]
			default:
				nbrs = append(append([]string{}, out[id]...), in[id]...)
			}
			for _, n := range nbrs {
				if !visited[n] {
					visited[n] = true
					next = append(next, n)
				}
			}
		}
		sort.Strings(next)
		ordered = append(ordered, next...)
		frontier = next
		if len(next) == 0 {
			break
		}
	}

	// Unresolved targets attached to in-scope sources (separately typed).
	type uNode struct{ id, label, path, revision string }
	unresBySource := map[string][]uNode{}
	if q.Direction != "in" && (q.Scope == "vault" || q.Scope == "folder" || q.Depth > 0) {
		for _, l := range links {
			if !visited[l.SourceID] || (l.State != "unresolved" && l.State != "ambiguous") {
				continue
			}
			target := l.TargetPath
			if target == "" {
				target = l.TargetBasename
			}
			if target == "" {
				continue
			}
			if !strings.HasSuffix(strings.ToLower(target), ".md") {
				target += ".md"
			}
			label := target
			if i := strings.LastIndex(label, "/"); i >= 0 {
				label = label[i+1:]
			}
			label = strings.TrimSuffix(label, ".md")
			if q.Scope == "folder" && strings.Contains(target, "/") {
				prefix := strings.Trim(q.ScopePath, "/")
				if !strings.HasPrefix(target, prefix+"/") && target != prefix {
					continue
				}
			}
			unresBySource[l.SourceID] = append(unresBySource[l.SourceID], uNode{"u:" + strings.ToLower(target), label, target, l.SourceRevision})
		}
	}

	// Matched counts describe the whole authorized scope before caps (distinct pairs).
	var matchedEdges int
	{
		pair := map[string]bool{}
		for src := range visited {
			for _, dst := range out[src] {
				if visited[dst] {
					pair[src+">"+dst] = true
				}
			}
		}
		matchedEdges = len(pair)
	}

	// Apply output caps in traversal order (closest first).
	nodeCap := q.Limit
	if nodeCap > analysisHardNodeCap {
		nodeCap = analysisHardNodeCap
	}
	returned := ordered
	truncated := false
	reason := ""
	if len(returned) > nodeCap {
		returned = returned[:nodeCap]
		truncated = true
		reason = "node-limit"
	}
	inReturned := map[string]bool{}
	for _, id := range returned {
		inReturned[id] = true
	}

	// Keep directed pairs distinct. Degree is computed from the final graph
	// below, so reciprocal links and repeated mentions share one neighbor.
	edgeSeen := map[string]bool{}
	for _, src := range returned {
		for _, dst := range out[src] {
			if inReturned[dst] && src != dst {
				edgeSeen[src+">"+dst] = true
			}
		}
	}

	resp := &ports.AnalysisGraph{
		Version: 1,
		Query:   *q,
		Status:  "ready",
		Nodes:   []ports.AnalysisNode{},
		Edges:   []ports.AnalysisEdge{},
		Matched: ports.AnalysisCounts{Nodes: len(visited), Edges: matchedEdges},
		Limits:  ports.AnalysisLimits{Nodes: nodeCap, Edges: analysisHardEdgeCap},
	}

	// Build document nodes.
	for _, id := range returned {
		d := byID[id]
		label := d.Title
		if label == "" {
			label = strings.TrimSuffix(d.Path, ".md")
			if i := strings.LastIndex(label, "/"); i >= 0 {
				label = label[i+1:]
			}
		}
		resp.Nodes = append(resp.Nodes, ports.AnalysisNode{
			ID: id, Kind: "document", Label: label, DocumentID: id, Path: d.Path,
			Tags: d.Tags, Metric: "Degree",
		})
	}

	// Build edges with evidence (deduped pair, evidence capped per edge).
	linksBySource := map[string][]*ports.LinkRow{}
	for i := range links {
		l := &links[i]
		linksBySource[l.SourceID] = append(linksBySource[l.SourceID], l)
	}
	evidenceByPair := map[string][]ports.GraphEvidence{}
	order := []string{}
	for _, src := range returned {
		for _, l := range linksBySource[src] {
			if l.State != "resolved" || l.ResolvedID == "" || !inReturned[l.ResolvedID] || l.ResolvedID == src {
				continue
			}
			key := src + ">" + l.ResolvedID
			if !edgeSeen[key] {
				continue
			}
			ev := ports.GraphEvidence{
				DocumentID: src, Path: byID[src].Path, Revision: l.SourceRevision,
				Locator: l.Fragment, Method: "explicit-link",
				Stale: l.SourceRevision != "" && l.SourceRevision != byID[src].Revision,
			}
			if len(evidenceByPair[key]) < 8 {
				if _, ok := evidenceByPair[key]; !ok {
					order = append(order, key)
				}
				evidenceByPair[key] = append(evidenceByPair[key], ev)
			}
		}
	}
	sort.Strings(order)
	edgeCount := 0
	for _, key := range order {
		if len(resp.Edges) >= analysisHardEdgeCap {
			truncated = true
			reason = firstNonEmpty(reason, "edge-limit")
			break
		}
		i := strings.Index(key, ">")
		src, dst := key[:i], key[i+1:]
		resp.Edges = append(resp.Edges, ports.AnalysisEdge{
			ID: "e:" + strings.ToLower(key), Source: src, Target: dst,
			Type: "explicit-link", Weight: len(evidenceByPair[key]), Evidence: evidenceByPair[key],
		})
		edgeCount++
	}
	_ = edgeCount

	// Count unresolved targets in the authorized scope before output caps.
	matchedUnresolved := map[string]bool{}
	matchedUnresolvedEdges := map[string]bool{}
	for src, items := range unresBySource {
		for _, u := range items {
			matchedUnresolved[u.id] = true
			matchedUnresolvedEdges[src+">"+u.id] = true
		}
	}
	// Matched retains its v1 meaning: document nodes and resolved explicit
	// pairs. Report unresolved candidates separately so callers can explain
	// why Returned contains a different kind without breaking v1 consumers.
	resp.UnresolvedMatched = ports.AnalysisCounts{Nodes: len(matchedUnresolved), Edges: len(matchedUnresolvedEdges)}
	// Attach unresolved targets only when both node and edge caps permit them.
	if len(unresBySource) > 0 {
		seen := map[string]bool{}
		seenEdge := map[string]bool{}
		for _, src := range returned {
			for _, u := range unresBySource[src] {
				if !seen[u.id] {
					if len(resp.Nodes) >= nodeCap {
						truncated = true
						reason = firstNonEmpty(reason, "node-limit")
						continue
					}
					seen[u.id] = true
					resp.Nodes = append(resp.Nodes, ports.AnalysisNode{ID: u.id, Kind: "unresolved", Label: u.label, Metric: "Degree"})
				}
				if !seen[u.id] {
					continue
				}
				ek := src + ">" + u.id
				if seenEdge[ek] {
					continue
				}
				if len(resp.Edges) >= analysisHardEdgeCap {
					truncated = true
					reason = firstNonEmpty(reason, "edge-limit")
					continue
				}
				seenEdge[ek] = true
				resp.Edges = append(resp.Edges, ports.AnalysisEdge{
					ID: "e:" + strings.ToLower(ek), Source: src, Target: u.id,
					Type: "explicit-link", Weight: 1,
					Evidence: []ports.GraphEvidence{{DocumentID: src, Path: byID[src].Path, Revision: u.revision, Locator: u.path, Method: "explicit-link", Stale: u.revision != "" && u.revision != byID[src].Revision}},
				})
			}
		}
	}
	neighbors := map[string]map[string]bool{}
	for _, edge := range resp.Edges {
		if neighbors[edge.Source] == nil {
			neighbors[edge.Source] = map[string]bool{}
		}
		if neighbors[edge.Target] == nil {
			neighbors[edge.Target] = map[string]bool{}
		}
		neighbors[edge.Source][edge.Target] = true
		neighbors[edge.Target][edge.Source] = true
	}
	for i := range resp.Nodes {
		resp.Nodes[i].Importance = len(neighbors[resp.Nodes[i].ID])
	}

	resp.Returned = ports.AnalysisCounts{Nodes: len(resp.Nodes), Edges: len(resp.Edges)}
	resp.Truncated = truncated
	resp.Reason = reason
	return resp, nil
}

func normalizeAnalysisQuery(q *ports.GraphQuery) error {
	q.Query = strings.TrimSpace(q.Query)
	if len(q.Query) > 400 {
		return domain.Invalid("graph query exceeds 400 bytes")
	}
	switch q.Scope {
	case "vault":
	case "note":
		if len(q.ScopeIDs) != 1 {
			return domain.Invalid("note scope needs exactly one scopeId")
		}
	case "selection":
		if len(q.ScopeIDs) == 0 {
			return domain.Invalid("selection scope needs at least one scopeId")
		}
		if len(q.ScopeIDs) > 500 {
			return domain.Invalid("selection scope supports at most 500 notes")
		}
	case "folder":
		if strings.TrimSpace(q.ScopePath) == "" {
			return domain.Invalid("folder scope needs scopePath")
		}
	default:
		return domain.Invalid("scope must be one of vault, note, selection, folder")
	}
	if q.Mode == "" {
		q.Mode = "document"
	}
	if q.Mode != "document" {
		return domain.Invalid("mode document is the only analysis mode currently supported")
	}
	switch q.Direction {
	case "", "both", "in", "out":
		if q.Direction == "" {
			q.Direction = "both"
		}
	default:
		return domain.Invalid("direction must be one of in, out, both")
	}
	if q.Depth == -1 {
		if q.Scope == "note" {
			q.Depth = 1 // default local view: 1 hop (C01/C03); explicit 0 stays root-only
		} else {
			q.Depth = 0
		}
	}
	if q.Depth < 0 || q.Depth > 3 {
		return domain.Invalid("depth must be between 0 and 3")
	}
	if q.Limit < 0 {
		return domain.Invalid("limit must be positive")
	}
	if q.Limit == 0 || q.Limit > analysisHardNodeCap {
		q.Limit = analysisDefaultNodes
	}
	// selection/folder start from the exact seed set; neighbors need explicit depth.
	return nil
}

func validateAnalysisScope(q *ports.GraphQuery, byID map[string]*ports.DocLite) error {
	for _, id := range q.ScopeIDs {
		if byID[id] == nil {
			return domain.NotFound("scope document not found in this vault")
		}
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
