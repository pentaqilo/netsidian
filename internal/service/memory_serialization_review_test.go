package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMemoryReviewPreservesNestedUnknownMetadataAndCRLF(t *testing.T) {
	original := "---\r\ntitle: Human index\r\n---\r\nUnrelated prose.\r\n  - memory: {\"key\":\"a\",\"text\":\"Old\",\"custom\":{\"large\":9007199254740993},\"sourceRefs\":[{\"target\":\"https://example.test\",\"kind\":\"external\",\"customSource\":{\"retain\":true}}]}\r\n- memory: {\"key\":\"b\",\"text\":\"Other\",\"format\": [1, 2]}\r\n"
	entries := parseMemoryIndex(original)
	if len(entries) != 2 {
		t.Fatal(entries)
	}
	changed := *entries[0]
	changed.text = "New"
	merged, ok := mergeMemoryIndex(original, &changed)
	if !ok {
		t.Fatal("expected update")
	}
	if !strings.HasPrefix(merged, "---\r\ntitle: Human index\r\n---\r\nUnrelated prose.\r\n  - memory: ") || !strings.HasSuffix(merged, "- memory: {\"key\":\"b\",\"text\":\"Other\",\"format\": [1, 2]}\r\n") {
		t.Fatal("unrelated bytes/indentation/newlines changed", merged)
	}
	line := strings.Split(merged, "\r\n")[4]
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "- memory: ")), &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["custom"]) != "{\"large\":9007199254740993}" || !strings.Contains(string(fields["sourceRefs"]), "customSource") {
		t.Fatal("unknown nested metadata lost", string(fields["sourceRefs"]), string(fields["custom"]))
	}
	again, changedAgain := mergeMemoryIndex(merged, parseMemoryIndex(merged)[0])
	if changedAgain || again != merged {
		t.Fatal("semantic no-op must preserve exact bytes")
	}
}
