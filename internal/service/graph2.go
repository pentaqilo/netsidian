package service

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

type Graph2Node struct {
	ports.GraphNode
	Kind string `json:"kind"`
}
type Graph2Data struct {
	Nodes []Graph2Node      `json:"nodes"`
	Edges []ports.GraphEdge `json:"edges"`
	Total int               `json:"total"`
}

// Graph2 returns the complete lightweight link index. It deliberately performs
// no community analysis, content reads or 1,000-node sampling.
func (s *Services) Graph2(ctx context.Context, a *Actor, spaceID string) (*Graph2Data, error) {
	if _, err := s.requireReadScope(ctx, a, spaceID, domain.ScopeGraphRead); err != nil {
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
	g := &Graph2Data{Nodes: []Graph2Node{}, Edges: []ports.GraphEdge{}, Total: len(docs)}
	nodes := map[string]int{}
	for _, d := range docs {
		title := d.Title
		if title == "" {
			title = strings.TrimSuffix(path.Base(d.Path), ".md")
		}
		nodes[d.ID] = len(g.Nodes)
		g.Nodes = append(g.Nodes, Graph2Node{GraphNode: ports.GraphNode{ID: d.ID, Path: d.Path, Title: title, Tags: d.Tags, Memory: d.Memory}, Kind: "document"})
	}
	seen := map[string]bool{}
	for _, l := range links {
		if _, ok := nodes[l.SourceID]; !ok {
			continue
		}
		target := l.ResolvedID
		if l.State == "unresolved" || l.State == "ambiguous" {
			name := l.TargetPath
			if name == "" {
				name = l.TargetBasename
			}
			if name == "" {
				continue
			}
			target = "unresolved:" + shaHex([]byte(strings.ToLower(name)))[:20]
			if _, ok := nodes[target]; !ok {
				nodes[target] = len(g.Nodes)
				g.Nodes = append(g.Nodes, Graph2Node{GraphNode: ports.GraphNode{ID: target, Path: name, Title: strings.TrimSuffix(path.Base(name), ".md")}, Kind: "unresolved"})
			}
		} else if l.State != "resolved" {
			continue
		}
		if _, ok := nodes[target]; !ok || target == l.SourceID {
			continue
		}
		key := l.SourceID + "\x00" + target
		if seen[key] {
			continue
		}
		seen[key] = true
		g.Edges = append(g.Edges, ports.GraphEdge{Source: l.SourceID, Target: target})
		g.Nodes[nodes[l.SourceID]].Links++
		g.Nodes[nodes[target]].Links++
	}
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].Source == g.Edges[j].Source {
			return g.Edges[i].Target < g.Edges[j].Target
		}
		return g.Edges[i].Source < g.Edges[j].Source
	})
	return g, nil
}
