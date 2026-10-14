package service

import (
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"gopkg.in/yaml.v3"
)

// Board operations own only their declared fields. Tags and other frontmatter,
// as well as the explanatory document body, survive ordering/settings edits.
func renderBoardPreserving(original string, cfg BoardConfig) (string, error) {
	fm, body, diagnostics := markdown.ParseFrontmatter(original)
	if len(diagnostics) > 0 {
		return "", domain.Invalid("repair the board frontmatter before editing its settings")
	}
	rendered, err := renderBoardMarkdown(cfg, "")
	if err != nil {
		return "", err
	}
	own, _, _ := markdown.ParseFrontmatter(rendered)
	for _, key := range []string{boardMarkerKey, "version", "title", "columns", "order", "createDefaults"} {
		delete(fm, key)
		if value, ok := own[key]; ok {
			fm[key] = value
		}
	}
	head, err := yaml.Marshal(fm)
	if err != nil {
		return "", domain.Invalid("could not serialize board metadata")
	}
	return "---\n" + string(head) + "---\n" + body, nil
}
