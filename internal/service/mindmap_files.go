package service

import (
	"context"
	"path"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
)

const mindMapLinksStart = "<!-- netsidian:mindmap-links -->"
const mindMapLinksEnd = "<!-- /netsidian:mindmap-links -->"

// Membership links are normal Markdown: the map appears in backlinks and graph
// without turning visual connectors into unexpected edits to referenced notes.
func (s *Services) renderMindMapCanvas(ctx context.Context, a *Actor, space, original string, cfg MindMapConfig) (string, error) {
	rendered, err := renderMindMapPreserving(original, cfg)
	if err != nil {
		return "", err
	}
	var links []string
	for _, n := range cfg.Nodes {
		d, err := s.Docs.Document(ctx, n.ID)
		if err != nil {
			return "", err
		}
		if d != nil && d.SpaceID == space && d.TombstoneAt == nil {
			links = append(links, "- ["+strings.NewReplacer("[", "\\[", "]", "\\]").Replace(domain.BaseName(d.Path))+"](/"+escapeMindMapPath(d.Path)+")")
		}
	}
	start := strings.Index(rendered, mindMapLinksStart)
	end := strings.Index(rendered, mindMapLinksEnd)
	if start >= 0 && end >= start {
		rendered = rendered[:start] + rendered[end+len(mindMapLinksEnd):]
	}
	return strings.TrimRight(rendered, "\n") + "\n\n" + mindMapLinksStart + "\n" + strings.Join(links, "\n") + "\n" + mindMapLinksEnd + "\n", nil
}

func escapeMindMapPath(path string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", "(", "%28", ")", "%29", "#", "%23", "?", "%3F", "[", "%5B", "]", "%5D").Replace(path)
}

// MoveFileIntoMindMapNode groups a document under a stable node-specific folder.
// It never changes canvas membership of the moved file, and links are repaired
// through the same journaled move used by Kanban. The node document gets a normal
// Markdown link so its collected files also participate in backlinks and graph.
func (s *Services) MoveFileIntoMindMapNode(ctx context.Context, a *Actor, space, mapPath, nodeID, docID, revision, fromPath string) (*MindMap, []string, error) {
	lock := s.docLock("mindmap:" + space)
	lock.Lock()
	defer lock.Unlock()
	if _, err := s.Require(ctx, a, space, domain.ActionEdit); err != nil {
		return nil, nil, err
	}
	if revision == "" {
		return nil, nil, domain.PreconditionRequired("document revision is required")
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, nil, perr
	}
	mapDoc, _, cfg, err := s.loadMindMapConfig(ctx, a, space, canon)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := findMindMapNode(cfg.Nodes, nodeID); !ok {
		return nil, nil, domain.NotFound("node not found")
	}
	node, err := s.Docs.Document(ctx, nodeID)
	if err != nil {
		return nil, nil, err
	}
	if node == nil || node.SpaceID != space || node.TombstoneAt != nil || !isMindMapDocKind(node.Kind) {
		return nil, nil, domain.NotFound("node document unavailable")
	}
	if nodeID == docID {
		return nil, nil, domain.Invalid("a node cannot contain its own document")
	}
	doc, err := s.Docs.Document(ctx, docID)
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || doc.SpaceID != space || doc.TombstoneAt != nil {
		return nil, nil, domain.NotFound("document not found")
	}
	if !isMindMapDocKind(doc.Kind) || doc.ID == mapDoc.ID {
		return nil, nil, domain.Invalid("choose an ordinary Markdown document")
	}
	source, err := s.ReadNote(ctx, a, space, docID)
	if err != nil {
		return nil, nil, err
	}
	if source.Revision != revision {
		return nil, nil, domain.PreconditionFailed(source.Revision)
	}
	if fromPath == "" || source.Path != fromPath {
		return nil, nil, domain.Conflict("document moved; reload before moving it")
	}
	if strings.EqualFold(path.Base(doc.Path), mindMapFileName) || strings.EqualFold(path.Base(doc.Path), boardFileName) {
		return nil, nil, domain.Invalid("configuration files cannot be collected inside a node")
	}
	// Validate the ID before using it as a folder segment (normally an opaque doc_ ID).
	if strings.ContainsAny(nodeID, "/\\") || strings.HasPrefix(nodeID, ".") {
		return nil, nil, domain.Invalid("invalid node id")
	}
	next := canon + "/Nodes/" + nodeID + "/" + path.Base(doc.Path)
	if source.Path == next {
		return s.GetMindMapWithWarnings(ctx, a, space, canon, nil)
	}
	warnings, err := s.moveBoardDocument(ctx, a, space, doc, source, next, revision)
	if err != nil {
		return nil, nil, err
	}
	parent, err := s.ReadNote(ctx, a, space, nodeID)
	if err == nil {
		link := "[" + strings.NewReplacer("[", "\\[", "]", "\\]").Replace(domain.BaseName(next)) + "](/" + escapeMindMapPath(next) + ")"
		if !strings.Contains(parent.Markdown, link) {
			_, err = s.SaveContent(ctx, a, space, nodeID, parent.Revision, strings.TrimRight(parent.Markdown, "\n")+"\n\n- "+link+"\n", "", shaHex([]byte(parent.Markdown)))
		}
	}
	if err != nil {
		warnings = append(warnings, "File moved, but its link could not be added to the node document: "+err.Error())
	}
	return s.GetMindMapWithWarnings(ctx, a, space, canon, warnings)
}

func (s *Services) GetMindMapWithWarnings(ctx context.Context, a *Actor, space, path string, warnings []string) (*MindMap, []string, error) {
	m, err := s.GetMindMap(ctx, a, space, path)
	return m, warnings, err
}

// LinkFileToMindMapNode adds a normal Markdown link without relocating either
// file. The node revision is mandatory: linking cannot overwrite an editor draft.
func (s *Services) LinkFileToMindMapNode(ctx context.Context, a *Actor, space, mapPath, nodeID, docID, attachmentPath, revision string) (*MindMap, error) {
	lock := s.docLock("mindmap:" + space)
	lock.Lock()
	defer lock.Unlock()
	if _, err := s.Require(ctx, a, space, domain.ActionEdit); err != nil {
		return nil, err
	}
	if revision == "" {
		return nil, domain.PreconditionRequired("node revision is required")
	}
	canon, _, perr := domain.NormalizePath(mapPath)
	if perr != nil {
		return nil, perr
	}
	_, _, cfg, err := s.loadMindMapConfig(ctx, a, space, canon)
	if err != nil {
		return nil, err
	}
	if _, ok := findMindMapNode(cfg.Nodes, nodeID); !ok {
		return nil, domain.NotFound("node not found")
	}
	if (docID == "") == (attachmentPath == "") {
		return nil, domain.Invalid("choose one document or attachment")
	}
	var target, title string
	if docID != "" {
		d, err := s.Docs.Document(ctx, docID)
		if err != nil {
			return nil, err
		}
		if d == nil || d.SpaceID != space || d.TombstoneAt != nil {
			return nil, domain.NotFound("document not found")
		}
		if d.ID == nodeID {
			return nil, domain.Invalid("a node cannot link itself")
		}
		target = d.Path
		title = domain.BaseName(target)
	} else {
		clean, _, pathErr := domain.NormalizePath(attachmentPath)
		if pathErr != nil {
			return nil, pathErr
		}
		items, err := s.ListAttachments(ctx, a, space, clean)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.Path == clean {
				target = clean
				break
			}
		}
		if target == "" {
			return nil, domain.NotFound("attachment not found")
		}
		title = path.Base(target)
	}
	parent, err := s.ReadNote(ctx, a, space, nodeID)
	if err != nil {
		return nil, err
	}
	if parent.Revision != revision {
		return nil, domain.PreconditionFailed(parent.Revision)
	}
	destination := "(/" + escapeMindMapPath(target) + ")"
	if !strings.Contains(parent.Markdown, "]"+destination) {
		label := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(title)
		_, err = s.SaveContent(ctx, a, space, nodeID, revision, strings.TrimRight(parent.Markdown, "\n")+"\n\n- ["+label+"]"+destination+"\n", "", shaHex([]byte(parent.Markdown)))
		if err != nil {
			return nil, err
		}
	}
	return s.GetMindMap(ctx, a, space, canon)
}
