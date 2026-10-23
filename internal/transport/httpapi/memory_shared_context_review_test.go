package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestMemorySharedContextReview(t *testing.T) {
	e, _, _, token := mcpFixture(t)
	ctx := context.Background()
	session := connectMCP(t, e, token)
	records := []map[string]any{}
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		started := time.Now()
		result, out := mcpCall(t, session, name, args)
		elapsed := time.Since(started)
		if result.IsError {
			t.Fatal(name, out)
		}
		data, _ := json.Marshal(result)
		records = append(records, map[string]any{"tool": name, "elapsedMicros": elapsed.Microseconds(), "utf8Bytes": len(data), "result": result})
		return out
	}
	source := call("netsidian_document_create", map[string]any{"path": "Projects/Decision.md", "markdown": "# Accepted decision\nSQLite supports offline work.", "idempotencyKey": "shared-create"})
	input := map[string]any{"key": "project.storage", "text": "Use the accepted offline database decision.", "scope": "project", "refs": []string{source["documentId"].(string)}, "idempotencyKey": "shared-remember"}
	receipt := call("netsidian_memory_remember", input)
	session = connectMCP(t, e, token)
	recall := call("netsidian_memory_recall", map[string]any{"key": "project.storage", "maxBytes": 2048})
	if len(recall["entries"].([]any)) != 1 {
		t.Fatal(recall)
	}
	call("netsidian_document_read", map[string]any{"documentId": source["documentId"], "maxBytes": 2048})
	replay := call("netsidian_memory_remember", input)
	if replay["indexRevision"] != receipt["indexRevision"] {
		t.Fatal("duplicate retry changed revision")
	}
	// Keep context alive explicitly; all writes occur only in this isolated fixture.
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("NETSIDIAN_TEST_SHARED_CONTEXT_OUTPUT"); path != "" {
		data, _ := json.Marshal(records)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for _, record := range records {
		total += record["utf8Bytes"].(int)
	}
	t.Logf("Shared baseline fixture: %d calls, %d serialized result bytes, no redundant receipt read-back", len(records), total)
}
