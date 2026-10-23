package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

func TestMemoryPublicInputRejectsServerComputedMetadata(t *testing.T) {
	e, _, vault, token := mcpFixture(t)
	bodies := []string{`null`, `[]`, `{"key":"a","text":"Claim","schemaVersion":2}`, `{"key":"a","text":"Claim","sourceRefs":[{"target":"https://example.test","kind":"external","verified":true}]}`, `{"key":"a","text":"Claim","derivedFrom":[{"key":"old","fingerprint":"forged"}]}`, `{"key":"a","text":"One"}{"key":"b","text":"Two"}`}
	for _, body := range bodies {
		response := e.callHdrs(t, "POST", "/api/v1/spaces/"+vault+"/agent/memory/remember", body, token, nil)
		response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatalf("strict memory request accepted: status=%d body=%s", response.StatusCode, body)
		}
	}
}
func TestMemoryGenericAdapterAllowsReadOnlyRecall(t *testing.T) {
	e, owner, vault, _ := mcpFixture(t)
	ctx := context.Background()
	_, err := e.svc.RememberMemory(ctx, owner, vault, &service.MemoryRememberInput{Key: "a", Text: "Current claim"}, "generic-seed")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := e.svc.CreateKey(ctx, owner, "Memory reader", []string{vault}, []string{domain.ScopeDocumentsRead}, 1)
	if err != nil {
		t.Fatal(err)
	}
	session := connectMCP(t, e, token)
	result, out := mcpCall(t, session, "netsidian_read", mcpAPIInput{Operation: "POST /api/v1/spaces/{space}/agent/memory/recall", PathParameters: map[string]string{"space": vault}, Body: map[string]any{"key": "a"}})
	if result.IsError || out["status"] != float64(200) {
		t.Fatal("read-only POST recall incorrectly classified as mutation", out)
	}
	data, _ := json.Marshal(out["body"])
	var recalled service.MemoryRecallResult
	if err := json.Unmarshal(data, &recalled); err != nil || len(recalled.Entries) != 1 {
		t.Fatal(recalled, err)
	}
}
