package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
	"gopkg.in/yaml.v3"
)

const memoryManagedStart = "<!-- netsidian:memory-derived:start -->"
const memoryManagedEnd = "<!-- netsidian:memory-derived:end -->"

func claimFingerprint(line *memoryLine) string {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(strings.TrimPrefix(line.render(), "- memory: ")), &fields)
	delete(fields, "status")
	delete(fields, "schemaVersion")
	delete(fields, "updatedAt")
	delete(fields, "recordedAt")
	data, _ := json.Marshal(fields)
	return shaHex(data)
}
func memoryStatus(status string) string {
	if status == "" {
		return "active"
	}
	return status
}
func yamlMapValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
func setYAMLMap(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
func encodeYAMLValue(value any) *yaml.Node {
	// Public metadata uses JSON names; yaml's default struct naming differs.
	data, _ := json.Marshal(value)
	var portable any
	_ = json.Unmarshal(data, &portable)
	var node yaml.Node
	_ = node.Encode(portable)
	return &node
}

// Only the explicitly owned block is replaced; human Markdown before/after it
// and other frontmatter values/comments remain intact. Adoption appends a block.
func managedMemoryTarget(existing, body, operationID string, sources []MemorySourceRef, claims []MemoryClaimRef, adopt bool) (string, error) {
	if strings.Contains(body, memoryManagedStart) || strings.Contains(body, memoryManagedEnd) {
		return "", domain.Invalid("managed body must not contain ownership delimiters")
	}
	frontmatter := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	content := existing
	if strings.HasPrefix(existing, "---\n") || strings.HasPrefix(existing, "---\r\n") {
		first := strings.Index(existing, "\n") + 1
		rest := existing[first:]
		end := -1
		offset := 0
		for _, line := range strings.SplitAfter(rest, "\n") {
			if strings.TrimSpace(line) == "---" || strings.TrimSpace(line) == "..." {
				end = offset
				offset += len(line)
				break
			}
			offset += len(line)
		}
		if end < 0 {
			return "", domain.Invalid("target frontmatter is incomplete")
		}
		var document yaml.Node
		if yaml.Unmarshal([]byte(rest[:end]), &document) != nil || len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
			return "", domain.Invalid("target frontmatter must be a YAML mapping")
		}
		frontmatter = document.Content[0]
		content = rest[offset:]
	}
	namespace := yamlMapValue(frontmatter, "netsidian")
	if namespace == nil {
		namespace = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setYAMLMap(frontmatter, "netsidian", namespace)
	}
	if namespace.Kind != yaml.MappingNode {
		return "", domain.Invalid("existing netsidian frontmatter is not a mapping; preserve and repair it before adoption")
	}
	marker := yamlMapValue(namespace, "derived")
	if marker == nil {
		marker = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setYAMLMap(namespace, "derived", marker)
	}
	if marker.Kind != yaml.MappingNode {
		return "", domain.Invalid("existing derived frontmatter is not a mapping")
	}
	starts, ends := strings.Count(content, memoryManagedStart), strings.Count(content, memoryManagedEnd)
	if starts != ends || starts > 1 {
		return "", domain.Invalid("target has ambiguous or incomplete managed sections")
	}
	if version := yamlMapValue(marker, "schemaVersion"); version != nil && version.Value != "1" {
		return "", domain.Unsupported("derived marker uses an unsupported schema version; do not rewrite it")
	}
	owned := yamlMapValue(marker, "managed")
	isOwned := owned != nil && owned.Tag == "!!bool" && owned.Value == "true"
	if !adopt && (!isOwned || starts != 1) {
		return "", domain.Invalid("target is not agent-managed; explicitly adopt a new managed section to preserve human content")
	}
	block := memoryManagedStart + "\n" + body + "\n" + memoryManagedEnd
	if starts == 1 {
		start, end := strings.Index(content, memoryManagedStart), strings.Index(content, memoryManagedEnd)
		if end < start {
			return "", domain.Invalid("target ownership delimiters are inverted")
		}
		content = content[:start] + block + content[end+len(memoryManagedEnd):]
	} else {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if content != "" {
			content += "\n"
		}
		content += block + "\n"
	}
	setYAMLMap(marker, "schemaVersion", encodeYAMLValue(1))
	setYAMLMap(marker, "managed", encodeYAMLValue(true))
	setYAMLMap(marker, "operationId", encodeYAMLValue(operationID))
	sourceData := []map[string]string{}
	for _, source := range sources {
		sourceData = append(sourceData, map[string]string{"documentId": source.DocumentID, "revision": source.Revision})
	}
	setYAMLMap(marker, "sources", encodeYAMLValue(sourceData))
	setYAMLMap(marker, "claims", encodeYAMLValue(claims))
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(frontmatter); err != nil {
		return "", err
	}
	_ = encoder.Close()
	return "---\n" + out.String() + "---\n" + content, nil
}
