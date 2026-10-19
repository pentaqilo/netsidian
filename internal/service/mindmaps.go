package service

import (
	"context"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"gopkg.in/yaml.v3"
)

// Folder-backed mind maps. A mind map is a folder that contains a mindmap.md
// whose YAML frontmatter carries the versioned canvas configuration: node
// placement (x/y/size/color) and the connectors between nodes. Nodes are
// ordinary Markdown documents. A node whose document lives inside the map
// folder is "owned" by the map; a node whose document lives elsewhere is a
// reference — the canvas points at the file without moving it. Moving an
// existing file into the map is an explicit, separate operation that reuses
// the journaled rename and link-repair path shared with boards.
//
// A node's id is its document id: one node per document per map, stable
// across renames, and connectors survive document moves because they name
// document ids, not paths. The configuration never duplicates titles or
// paths — those are resolved from the documents themselves at read time.

// MindMapNode is one configured node of a mind map. ID is the document id.
type MindMapNode struct {
	ID    string  `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w,omitempty"`
	H     float64 `json:"h,omitempty"`
	Color string  `json:"color,omitempty"`
}

// MindMapEdge is one connector between two nodes.
type MindMapEdge struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Label    string `json:"label,omitempty"`
	Dashed   bool   `json:"dashed,omitempty"`
	FromSide string `json:"fromSide,omitempty"`
	ToSide   string `json:"toSide,omitempty"`
}

// MindMapConfig is the parsed, validated canvas configuration.
type MindMapConfig struct {
	Version int           `json:"version"`
	Title   string        `json:"title,omitempty"`
	Nodes   []MindMapNode `json:"nodes"`
	Edges   []MindMapEdge `json:"edges"`
}

// MindMapNodeView is a node resolved against the vault's documents.
type MindMapNodeView struct {
	MindMapNode
	Title     string    `json:"title"`
	Path      string    `json:"path,omitempty"`
	Revision  string    `json:"revision,omitempty"`
	Owned     bool      `json:"owned"`
	Missing   bool      `json:"missing,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	Warnings  []string  `json:"warnings,omitempty"`
}

// MindMapView is a connector resolved against the map's nodes.
type MindMapView struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Label   string `json:"label,omitempty"`
	Dashed  bool   `json:"dashed,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

// MindMap is the resolved mind map view returned to clients.
type MindMap struct {
	Path     string            `json:"path"`
	Title    string            `json:"title"`
	Revision string            `json:"revision"`
	Config   MindMapConfig     `json:"config"`
	Nodes    []MindMapNodeView `json:"nodes"`
	Edges    []MindMapView     `json:"edges"`
	Warnings []string          `json:"warnings,omitempty"`
}

// MindMapSummary is a lightweight listing entry.
type MindMapSummary struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Nodes int    `json:"nodes"`
	Edges int    `json:"edges"`
}

const (
	mindMapFileName  = "mindmap.md"
	mindMapMarkerKey = "netsidian"
	mindMapMarkerVal = "mindmap"
	maxMindMapNodes  = 200
	maxMindMapEdges  = 400
)

var mindMapColorRE = regexp.MustCompile(`^$|^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
var mindMapEdgeIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)

func mindMapMDPath(mapPath string) string {
	return mapPath + "/" + mindMapFileName
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// parseMindMapConfig reads the canvas configuration out of parsed frontmatter.
// It returns ok=false when the frontmatter is not a mind map configuration.
func parseMindMapConfig(fm map[string]any) (MindMapConfig, bool) {
	marker, _ := fm[mindMapMarkerKey].(string)
	if !strings.EqualFold(strings.TrimSpace(marker), mindMapMarkerVal) {
		return MindMapConfig{}, false
	}
	cfg := MindMapConfig{Version: 1}
	if v, ok := asInt(fm["version"]); ok && v > 0 {
		cfg.Version = v
	}
	if t, ok := fm["title"].(string); ok {
		cfg.Title = strings.TrimSpace(t)
	}
	if raw, ok := fm["nodes"]; ok {
		cfg.Nodes = parseMindMapNodes(raw)
	}
	if raw, ok := fm["edges"]; ok {
		cfg.Edges = parseMindMapEdges(raw)
	}
	if cfg.Nodes == nil {
		cfg.Nodes = []MindMapNode{}
	}
	if cfg.Edges == nil {
		cfg.Edges = []MindMapEdge{}
	}
	return cfg, true
}

func parseMindMapNodes(raw any) []MindMapNode {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]MindMapNode, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		node := MindMapNode{ID: id}
		if x, ok := asFloat(m["x"]); ok {
			node.X = x
		}
		if y, ok := asFloat(m["y"]); ok {
			node.Y = y
		}
		if w, ok := asFloat(m["w"]); ok {
			node.W = w
		}
		if h, ok := asFloat(m["h"]); ok {
			node.H = h
		}
		if c, ok := m["color"].(string); ok {
			node.Color = strings.TrimSpace(c)
		}
		out = append(out, node)
	}
	return out
}

func parseMindMapEdges(raw any) []MindMapEdge {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]MindMapEdge, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		id = strings.TrimSpace(id)
		from, _ := m["from"].(string)
		to, _ := m["to"].(string)
		if id == "" || strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
			continue
		}
		edge := MindMapEdge{ID: id, From: strings.TrimSpace(from), To: strings.TrimSpace(to)}
		if label, ok := m["label"].(string); ok {
			edge.Label = strings.TrimSpace(label)
		}
		if d, ok := m["dashed"].(bool); ok {
			edge.Dashed = d
		}
		edge.FromSide, _ = m["fromSide"].(string)
		edge.ToSide, _ = m["toSide"].(string)
		out = append(out, edge)
	}
	return out
}

// validateMindMapConfig enforces the structural invariants of a canvas config.
func validateMindMapConfig(cfg MindMapConfig) error {
	if cfg.Version != 1 {
		return domain.Unsupported("unsupported mind map configuration version")
	}
	if len(cfg.Nodes) > maxMindMapNodes {
		return domain.Invalid("a mind map supports at most 200 nodes")
	}
	if len(cfg.Edges) > maxMindMapEdges {
		return domain.Invalid("a mind map supports at most 400 connectors")
	}
	seen := map[string]bool{}
	for _, n := range cfg.Nodes {
		if n.ID == "" {
			return domain.Invalid("node id must not be empty")
		}
		if seen[n.ID] {
			return domain.Invalid("duplicate node for the same document: " + n.ID)
		}
		seen[n.ID] = true
		if math.IsNaN(n.X) || math.IsInf(n.X, 0) || math.IsNaN(n.Y) || math.IsInf(n.Y, 0) || math.Abs(n.X) > 1e6 || math.Abs(n.Y) > 1e6 {
			return domain.Invalid("node position must be a finite coordinate within bounds")
		}
		if n.W < 0 || n.W > 2000 || n.H < 0 || n.H > 2000 || math.IsNaN(n.W) || math.IsNaN(n.H) {
			return domain.Invalid("node size must be between 0 and 2000")
		}
		if !mindMapColorRE.MatchString(n.Color) {
			return domain.Invalid("node color must be empty or a #rgb/#rrggbb hex value")
		}
	}
	edgeIDs := map[string]bool{}
	for _, e := range cfg.Edges {
		if !mindMapEdgeIDRE.MatchString(e.ID) {
			return domain.Invalid("connector id must use letters, numbers, dots, underscores, colons or hyphens")
		}
		if edgeIDs[e.ID] {
			return domain.Invalid("duplicate connector id: " + e.ID)
		}
		edgeIDs[e.ID] = true
		if !validMindMapSide(e.FromSide) || !validMindMapSide(e.ToSide) {
			return domain.Invalid("invalid connector attachment side")
		}
		if e.From == e.To {
			return domain.Invalid("a connector cannot link a node to itself")
		}
		if !seen[e.From] || !seen[e.To] {
			return domain.Invalid("connector refers to an unknown node")
		}
		if len(e.Label) > 200 {
			return domain.Invalid("connector label must not exceed 200 characters")
		}
	}
	return nil
}

// mindMapFileYAML is the on-disk frontmatter shape for mindmap.md.
type mindMapNodeYAML struct {
	ID    string  `yaml:"id"`
	X     float64 `yaml:"x"`
	Y     float64 `yaml:"y"`
	W     float64 `yaml:"w,omitempty"`
	H     float64 `yaml:"h,omitempty"`
	Color string  `yaml:"color,omitempty"`
}
type mindMapEdgeYAML struct {
	ID       string `yaml:"id"`
	From     string `yaml:"from"`
	To       string `yaml:"to"`
	Label    string `yaml:"label,omitempty"`
	Dashed   bool   `yaml:"dashed,omitempty"`
	FromSide string `yaml:"fromSide,omitempty"`
	ToSide   string `yaml:"toSide,omitempty"`
}
type mindMapFileYAML struct {
	Netsidian string            `yaml:"netsidian"`
	Version   int               `yaml:"version"`
	Title     string            `yaml:"title,omitempty"`
	Nodes     []mindMapNodeYAML `yaml:"nodes"`
	Edges     []mindMapEdgeYAML `yaml:"edges"`
}

// renderMindMapMarkdown serializes a canvas config plus a preserved body.
func renderMindMapMarkdown(cfg MindMapConfig, body string) (string, error) {
	file := mindMapFileYAML{
		Netsidian: mindMapMarkerVal,
		Version:   cfg.Version,
		Title:     cfg.Title,
		Nodes:     []mindMapNodeYAML{},
		Edges:     []mindMapEdgeYAML{},
	}
	for _, n := range cfg.Nodes {
		file.Nodes = append(file.Nodes, mindMapNodeYAML{ID: n.ID, X: n.X, Y: n.Y, W: n.W, H: n.H, Color: n.Color})
	}
	for _, e := range cfg.Edges {
		file.Edges = append(file.Edges, mindMapEdgeYAML{ID: e.ID, From: e.From, To: e.To, Label: e.Label, Dashed: e.Dashed, FromSide: e.FromSide, ToSide: e.ToSide})
	}
	head, err := yaml.Marshal(file)
	if err != nil {
		return "", domain.Invalid("could not serialize mind map configuration: " + err.Error())
	}
	out := "---\n" + string(head) + "---\n"
	if body != "" {
		out += body
	}
	return out, nil
}

// isMindMapDocKind restricts nodes to note/memory documents; attachments and
// other supporting files never become nodes.
func isMindMapDocKind(kind string) bool {
	return kind == "note" || kind == "memory"
}

// readMindMapDocument resolves the mindmap.md document for a map folder.
func (s *Services) readMindMapDocument(ctx context.Context, a *Actor, spaceID, mapPath string) (*domain.Document, *NoteContent, error) {
	_, key, perr := domain.NormalizePath(mindMapMDPath(mapPath))
	if perr != nil {
		return nil, nil, perr
	}
	doc, err := s.Docs.DocumentByNormKey(ctx, spaceID, key)
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || doc.TombstoneAt != nil {
		return nil, nil, domain.NotFound("mind map not found")
	}
	nc, err := s.ReadNote(ctx, a, spaceID, doc.ID)
	if err != nil {
		return nil, nil, err
	}
	return doc, nc, nil
}

// loadMindMapConfig refuses malformed or unsupported data before mutation.
func (s *Services) loadMindMapConfig(ctx context.Context, a *Actor, spaceID, mapPath string) (*domain.Document, *NoteContent, MindMapConfig, error) {
	doc, nc, err := s.readMindMapDocument(ctx, a, spaceID, mapPath)
	if err != nil {
		return nil, nil, MindMapConfig{}, err
	}
	fm, _, diagnostics := markdown.ParseFrontmatter(nc.Markdown)
	if len(diagnostics) > 0 {
		return nil, nil, MindMapConfig{}, domain.Invalid("repair malformed mind map frontmatter in mindmap.md")
	}
	cfg, ok := parseMindMapConfig(fm)
	if !ok {
		return nil, nil, MindMapConfig{}, domain.NotFound("mind map not found")
	}
	if raw, exists := fm["version"]; exists {
		v, valid := asInt(raw)
		if !valid || v != 1 {
			return nil, nil, MindMapConfig{}, domain.Unsupported("unsupported mind map configuration version")
		}
	}
	for _, key := range []string{"nodes", "edges"} {
		raw, exists := fm[key]
		if !exists {
			continue
		}
		list, ok := raw.([]any)
		if !ok {
			return nil, nil, MindMapConfig{}, domain.Invalid("repair mind map " + key + ": expected an array")
		}
		parsed := len(cfg.Nodes)
		if key == "edges" {
			parsed = len(cfg.Edges)
		}
		if parsed != len(list) {
			return nil, nil, MindMapConfig{}, domain.Invalid("repair malformed mind map " + key)
		}
		for _, item := range list {
			m := item.(map[string]any)
			if key == "nodes" {
				for _, field := range []string{"x", "y", "w", "h"} {
					if raw, exists := m[field]; exists {
						if _, ok := asFloat(raw); !ok {
							return nil, nil, MindMapConfig{}, domain.Invalid("repair nonnumeric node " + field)
						}
					}
				}
			}
		}
	}
	if err := validateMindMapConfig(cfg); err != nil {
		return nil, nil, MindMapConfig{}, err
	}
	return doc, nc, cfg, nil
}

// GetMindMap resolves the full canvas view: nodes resolved against their
// documents (owned vs reference) plus connectors resolved against the nodes.
func (s *Services) GetMindMap(ctx context.Context, a *Actor, spaceID, mapPath string) (*MindMap, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, perr
	}
	_, nc, cfg, err := s.loadMindMapConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	docs, err := s.ListDocuments(ctx, a, spaceID, false)
	if err != nil {
		return nil, err
	}
	byID := map[string]*DocMeta{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	mindMD := mindMapMDPath(canon)
	var warnings []string
	nodes := make([]MindMapNodeView, 0, len(cfg.Nodes))
	present := map[string]bool{}
	for _, n := range cfg.Nodes {
		view := MindMapNodeView{MindMapNode: n}
		d := byID[n.ID]
		if d == nil || d.TombstoneAt != nil || !isMindMapDocKind(d.Kind) || d.Path == mindMD {
			view.Missing = true
			view.Title = "Missing document"
			view.Warnings = []string{"The document behind this node was trashed or is no longer a Markdown document."}
			warnings = append(warnings, "A node refers to a document that no longer exists. Remove the node to clear this warning.")
			nodes = append(nodes, view)
			continue
		}
		view.Title = d.Title
		if view.Title == "" {
			view.Title = domain.BaseName(d.Path)
		}
		view.Path = d.Path
		view.Revision = d.CurrentRevision
		view.UpdatedAt = d.UpdatedAt
		view.Owned = underPrefix(d.Path, canon)
		present[n.ID] = true
		nodes = append(nodes, view)
	}
	edges := make([]MindMapView, 0, len(cfg.Edges))
	for _, e := range cfg.Edges {
		view := MindMapView{ID: e.ID, From: e.From, To: e.To, Label: e.Label, Dashed: e.Dashed}
		if !present[e.From] || !present[e.To] {
			view.Missing = true
			warnings = append(warnings, "A connector refers to a node that is no longer on the canvas.")
		}
		edges = append(edges, view)
	}
	title := cfg.Title
	if title == "" {
		title = domain.BaseName(canon)
	}
	return &MindMap{
		Path: canon, Title: title, Revision: nc.Revision, Config: cfg,
		Nodes: nodes, Edges: edges, Warnings: warnings,
	}, nil
}

// ListMindMaps discovers every folder that carries a mindmap.md configuration.
func (s *Services) ListMindMaps(ctx context.Context, a *Actor, spaceID string) ([]MindMapSummary, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	docs, err := s.ListDocuments(ctx, a, spaceID, false)
	if err != nil {
		return nil, err
	}
	out := []MindMapSummary{}
	for _, d := range docs {
		if d.TombstoneAt != nil || d.Kind != "note" {
			continue
		}
		if !strings.HasSuffix(d.Path, "/"+mindMapFileName) {
			continue
		}
		mapPath := strings.TrimSuffix(d.Path, "/"+mindMapFileName)
		nc, err := s.ReadNote(ctx, a, spaceID, d.ID)
		if err != nil {
			continue
		}
		fm, _, _ := markdown.ParseFrontmatter(nc.Markdown)
		cfg, ok := parseMindMapConfig(fm)
		if !ok {
			continue
		}
		title := cfg.Title
		if title == "" {
			title = domain.BaseName(mapPath)
		}
		out = append(out, MindMapSummary{Path: mapPath, Title: title, Nodes: len(cfg.Nodes), Edges: len(cfg.Edges)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// CreateMindMap creates a map folder with a blank canvas configuration.
// Nodes and connectors are added from the canvas; the folder itself is the
// only structure the map imposes on the vault.
func (s *Services) CreateMindMap(ctx context.Context, a *Actor, spaceID, mapPath, title, idemKey string) (*MindMap, error) {
	mapLock := s.docLock("mindmap:" + spaceID)
	mapLock.Lock()
	defer mapLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionCreate); err != nil {
		return nil, err
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, perr
	}
	if err := validFolderRel(canon); err != nil {
		return nil, err
	}
	if canon == "" {
		return nil, domain.Invalid("mind map folder must not be the vault root")
	}
	maps, listErr := s.ListMindMaps(ctx, a, spaceID)
	if listErr != nil {
		return nil, listErr
	}
	for _, m := range maps {
		if m.Path != canon && (underPrefix(canon, m.Path) || underPrefix(m.Path, canon)) {
			return nil, domain.Invalid("owning mind maps cannot be nested; use a sibling folder")
		}
	}
	cfg := MindMapConfig{Version: 1, Title: strings.TrimSpace(title), Nodes: []MindMapNode{}, Edges: []MindMapEdge{}}
	body := "# " + cfg.Title + "\n\nThis file configures the mind map. Arrange nodes and connectors from the mind map view.\n"
	if cfg.Title == "" {
		body = "# Mind map\n\nThis file configures the mind map. Arrange nodes and connectors from the mind map view.\n"
	}
	content, err := renderMindMapMarkdown(cfg, body)
	if err != nil {
		return nil, err
	}
	if _, err := s.CreateNote(ctx, a, spaceID, mindMapMDPath(canon), content, idemKey, false); err != nil {
		return nil, err
	}
	return s.GetMindMap(ctx, a, spaceID, canon)
}

// UpdateMindMapConfigRequest carries the full canvas state. Positions,
// sizes, colors and connectors are replaced; a nil list keeps the old one.
type UpdateMindMapConfigRequest struct {
	Title string        `json:"title,omitempty"`
	Nodes []MindMapNode `json:"nodes,omitempty"`
	Edges []MindMapEdge `json:"edges,omitempty"`
}

// UpdateMindMapConfig saves the canvas configuration. Guarded by If-Match on
// the mindmap.md revision so concurrent canvas edits collide loudly instead
// of silently overwriting each other.
func (s *Services) UpdateMindMapConfig(ctx context.Context, a *Actor, spaceID, mapPath string, req UpdateMindMapConfigRequest, expectedRevision string) (*MindMap, error) {
	mapLock := s.docLock("mindmap:" + spaceID)
	mapLock.Lock()
	defer mapLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	if expectedRevision == "" {
		return nil, domain.PreconditionRequired("If-Match with the current mind map revision is required")
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, perr
	}
	doc, nc, oldCfg, err := s.loadMindMapConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	if nc.Revision != expectedRevision {
		return nil, domain.PreconditionFailed(nc.Revision)
	}
	newCfg := MindMapConfig{
		Version: oldCfg.Version,
		Title:   strings.TrimSpace(req.Title),
		Nodes:   oldCfg.Nodes,
		Edges:   oldCfg.Edges,
	}
	if newCfg.Title == "" {
		newCfg.Title = oldCfg.Title
	}
	if req.Nodes != nil {
		newCfg.Nodes = req.Nodes
	}
	if req.Edges != nil {
		newCfg.Edges = req.Edges
	}
	for _, n := range newCfg.Nodes {
		if _, present := findMindMapNode(oldCfg.Nodes, n.ID); present {
			continue
		}
		target, err := s.Docs.Document(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		if target == nil || target.SpaceID != spaceID || target.TombstoneAt != nil || !isMindMapDocKind(target.Kind) || target.ID == doc.ID {
			return nil, domain.Invalid("new nodes must reference existing Markdown documents in this vault")
		}
	}
	if err := validateMindMapConfig(newCfg); err != nil {
		return nil, err
	}
	content, err := s.renderMindMapCanvas(ctx, a, spaceID, nc.Markdown, newCfg)
	if err != nil {
		return nil, err
	}
	if _, err := s.SaveContent(ctx, a, spaceID, doc.ID, expectedRevision, content, "", shaHex([]byte(nc.Markdown))); err != nil {
		return nil, err
	}
	return s.GetMindMap(ctx, a, spaceID, canon)
}

// CreateMindMapNode creates a new owned Markdown document and places it on
// the canvas in one step.
func (s *Services) CreateMindMapNode(ctx context.Context, a *Actor, spaceID, mapPath, title string, x, y float64, idemKey string) (*MindMap, error) {
	mapLock := s.docLock("mindmap:" + spaceID)
	mapLock.Lock()
	defer mapLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionCreate); err != nil {
		return nil, err
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, perr
	}
	doc, nc, cfg, err := s.loadMindMapConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	if len(cfg.Nodes) >= maxMindMapNodes {
		return nil, domain.Invalid("a mind map supports at most 200 nodes")
	}
	name := strings.TrimSpace(title)
	if name == "" {
		return nil, domain.Invalid("node title must not be empty")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	safe, _, nameErr := domain.NormalizePath(name)
	if nameErr != nil {
		return nil, nameErr
	}
	if safe != name || strings.Contains(name, "/") || strings.HasPrefix(name, ".") {
		return nil, domain.Invalid("node title must be a file name without folders or leading dots")
	}
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) || math.Abs(x) > 1e6 || math.Abs(y) > 1e6 {
		return nil, domain.Invalid("node position must be a finite coordinate")
	}
	if strings.EqualFold(name, mindMapFileName) {
		return nil, domain.Invalid("mindmap.md is reserved for the canvas configuration")
	}
	content := "# " + strings.TrimSuffix(name, ".md") + "\n\n"
	meta, err := s.CreateNote(ctx, a, spaceID, canon+"/"+name, content, idemKey, false)
	if err != nil {
		return nil, err
	}
	for _, n := range cfg.Nodes {
		if n.ID == meta.ID {
			return s.GetMindMap(ctx, a, spaceID, canon)
		}
	}
	cfg.Nodes = append(cfg.Nodes, MindMapNode{ID: meta.ID, X: x, Y: y})
	if err := validateMindMapConfig(cfg); err != nil {
		return nil, err
	}
	content2, err := s.renderMindMapCanvas(ctx, a, spaceID, nc.Markdown, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := s.SaveContent(ctx, a, spaceID, doc.ID, nc.Revision, content2, "", shaHex([]byte(nc.Markdown))); err != nil {
		return nil, domain.Unavailable("node document created, but the canvas could not be updated; reload and link the existing document")
	}
	return s.GetMindMap(ctx, a, spaceID, canon)
}

// AddMindMapReference places an existing vault document on the canvas
// without moving it. The node stays a reference until the user explicitly
// moves the file into the map folder.
func (s *Services) AddMindMapReference(ctx context.Context, a *Actor, spaceID, mapPath, docID string, x, y float64) (*MindMap, error) {
	mapLock := s.docLock("mindmap:" + spaceID)
	mapLock.Lock()
	defer mapLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, perr
	}
	doc, nc, cfg, err := s.loadMindMapConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	target, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return nil, err
	}
	if target == nil || target.SpaceID != spaceID || target.TombstoneAt != nil {
		return nil, domain.NotFound("document not found")
	}
	if !isMindMapDocKind(target.Kind) {
		return nil, domain.Invalid("only Markdown documents can be mind map nodes")
	}
	if strings.EqualFold(target.Path, mindMapMDPath(canon)) {
		return nil, domain.Invalid("the mind map configuration file is not a node")
	}
	for _, n := range cfg.Nodes {
		if n.ID == target.ID {
			return nil, domain.Conflict("this document is already on the canvas")
		}
	}
	if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) || math.Abs(x) > 1e6 || math.Abs(y) > 1e6 {
		return nil, domain.Invalid("node position must be a finite coordinate")
	}
	cfg.Nodes = append(cfg.Nodes, MindMapNode{ID: target.ID, X: x, Y: y})
	if err := validateMindMapConfig(cfg); err != nil {
		return nil, err
	}
	content, err := s.renderMindMapCanvas(ctx, a, spaceID, nc.Markdown, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := s.SaveContent(ctx, a, spaceID, doc.ID, nc.Revision, content, "", shaHex([]byte(nc.Markdown))); err != nil {
		return nil, err
	}
	return s.GetMindMap(ctx, a, spaceID, canon)
}

// MoveDocIntoMindMap is the explicit move workflow: it relocates an existing
// document into the map folder (preserving identity/history and repairing
// links) and ensures the document has a node on the canvas.
func (s *Services) MoveDocIntoMindMap(ctx context.Context, a *Actor, spaceID, mapPath, docID, expectedRevision string, expectedPaths ...string) (*MindMap, []string, error) {
	mapLock := s.docLock("mindmap:" + spaceID)
	mapLock.Lock()
	defer mapLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, nil, err
	}
	if expectedRevision == "" {
		return nil, nil, domain.PreconditionRequired("If-Match with the current document revision is required")
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, nil, perr
	}
	mapDoc, nc, cfg, err := s.loadMindMapConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, nil, err
	}
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || doc.SpaceID != spaceID || doc.TombstoneAt != nil {
		return nil, nil, domain.NotFound("document not found")
	}
	if !isMindMapDocKind(doc.Kind) {
		return nil, nil, domain.Invalid("only Markdown documents can be mind map nodes")
	}
	source, err := s.ReadNote(ctx, a, spaceID, doc.ID)
	if err != nil {
		return nil, nil, err
	}
	fm, _, _ := markdown.ParseFrontmatter(source.Markdown)
	if _, isMap := parseMindMapConfig(fm); isMap && strings.EqualFold(path.Base(doc.Path), mindMapFileName) {
		return nil, nil, domain.Invalid("mind map configuration is not a node")
	}
	from := doc.Path
	if len(expectedPaths) > 0 && expectedPaths[0] != from {
		return nil, nil, domain.Conflict("document has moved; reload before moving it")
	}
	if source.Revision != expectedRevision {
		return nil, nil, domain.PreconditionFailed(source.Revision)
	}
	if underPrefix(from, canon) {
		return nil, nil, domain.Invalid("the document is already inside the mind map folder")
	}
	// Place the node first: if the rename below fails, the canvas simply keeps
	// the document as a reference node rather than losing the placement.
	if _, exists := findMindMapNode(cfg.Nodes, doc.ID); !exists {
		cfg.Nodes = append(cfg.Nodes, MindMapNode{ID: doc.ID, X: 0, Y: 0})
		if err := validateMindMapConfig(cfg); err != nil {
			return nil, nil, err
		}
		content, err := s.renderMindMapCanvas(ctx, a, spaceID, nc.Markdown, cfg)
		if err != nil {
			return nil, nil, err
		}
		if _, err := s.SaveContent(ctx, a, spaceID, mapDoc.ID, nc.Revision, content, "", shaHex([]byte(nc.Markdown))); err != nil {
			return nil, nil, err
		}
	}
	next := canon + "/" + path.Base(from)
	warnings, err := s.moveBoardDocument(ctx, a, spaceID, doc, source, next, expectedRevision)
	if err != nil {
		return nil, nil, err
	}
	m, err := s.GetMindMap(ctx, a, spaceID, canon)
	return m, warnings, err
}

// DeleteMindMapNode removes a node and its connectors from the canvas. When
// trash is requested and the node is owned, the document itself moves to
// the app trash; reference nodes are only unlinked.
func (s *Services) DeleteMindMapNode(ctx context.Context, a *Actor, spaceID, mapPath, nodeID string, expectedRevision string, trash bool) (*MindMap, error) {
	mapLock := s.docLock("mindmap:" + spaceID)
	mapLock.Lock()
	defer mapLock.Unlock()
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	if expectedRevision == "" {
		return nil, domain.PreconditionRequired("If-Match with the current mind map revision is required")
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, perr
	}
	doc, nc, cfg, err := s.loadMindMapConfig(ctx, a, spaceID, canon)
	if err != nil {
		return nil, err
	}
	if nc.Revision != expectedRevision {
		return nil, domain.PreconditionFailed(nc.Revision)
	}
	idx := -1
	for i, n := range cfg.Nodes {
		if n.ID == nodeID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, domain.NotFound("node not found on this canvas")
	}
	owned := false
	var trashTarget *domain.Document
	if trash {
		if _, err := s.Require(ctx, a, spaceID, domain.ActionTrash); err != nil {
			return nil, err
		}
		target, err := s.Docs.Document(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		if target != nil && target.TombstoneAt == nil && underPrefix(target.Path, canon) && !strings.EqualFold(target.Path, mindMapMDPath(canon)) {
			owned = true
			trashTarget = target
		}
	}
	cfg.Nodes = append(append([]MindMapNode{}, cfg.Nodes[:idx]...), cfg.Nodes[idx+1:]...)
	edges := make([]MindMapEdge, 0, len(cfg.Edges))
	for _, e := range cfg.Edges {
		if e.From != nodeID && e.To != nodeID {
			edges = append(edges, e)
		}
	}
	cfg.Edges = edges
	content, err := s.renderMindMapCanvas(ctx, a, spaceID, nc.Markdown, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := s.SaveContent(ctx, a, spaceID, doc.ID, expectedRevision, content, "", shaHex([]byte(nc.Markdown))); err != nil {
		return nil, err
	}
	if trash && owned {
		if err := s.Trash(ctx, a, spaceID, nodeID, trashTarget.CurrentRevision); err != nil {
			return nil, domain.Unavailable("node removed from the canvas, but trashing its document failed: " + err.Error())
		}
	}
	return s.GetMindMap(ctx, a, spaceID, canon)
}

func findMindMapNode(nodes []MindMapNode, id string) (MindMapNode, bool) {
	for _, n := range nodes {
		if n.ID == id {
			return n, true
		}
	}
	return MindMapNode{}, false
}

func validMindMapSide(side string) bool {
	return side == "" || side == "top" || side == "right" || side == "bottom" || side == "left"
}
