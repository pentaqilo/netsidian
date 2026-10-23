package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// Covers clients which preload all advertised schemas, including generic tools.
// Token counts are measured separately by the development tokenizer script.
func TestMemoryCatalogBudget(t *testing.T) {
	e, _, _, token := mcpFixture(t)
	session := connectMCP(t, e, token)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"tools": tools.Tools, "instructions": session.InitializeResult().Instructions})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Full authenticated tool catalog: %d serialized UTF-8 bytes, %d tools", len(data), len(tools.Tools))
	if len(data) > 32768 {
		t.Fatalf("tool catalog exceeds reviewed 32 KiB regression ceiling: %d", len(data))
	}
	if path := os.Getenv("NETSIDIAN_TEST_CATALOG_OUTPUT"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
