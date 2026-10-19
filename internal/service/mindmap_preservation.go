package service

import (
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"gopkg.in/yaml.v3"
)

// Mind map operations own only their declared fields. Tags and other
// frontmatter, as well as the explanatory document body, survive canvas
// edits (positions, connectors, colors, labels).
func renderMindMapPreserving(original string, cfg MindMapConfig) (string, error) {
	fm, body, diagnostics := markdown.ParseFrontmatter(original)
	if len(diagnostics) > 0 {
		return "", domain.Invalid("repair the mind map frontmatter before editing its canvas")
	}
	rendered, err := renderMindMapMarkdown(cfg, "")
	if err != nil {
		return "", err
	}
	own, _, _ := markdown.ParseFrontmatter(rendered)
	for _, key := range []string{mindMapMarkerKey, "version", "title", "nodes", "edges"} {
		delete(fm, key)
		if value, ok := own[key]; ok {
			fm[key] = value
		}
	}
	head, err := yaml.Marshal(fm)
	if err != nil {
		return "", domain.Invalid("could not serialize mind map metadata")
	}
	return "---\n" + string(head) + "---\n" + body, nil
}
