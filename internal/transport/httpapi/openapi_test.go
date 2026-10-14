package httpapi

// The published contract must mirror the live route table exactly:
// every registered route appears (annotated or generic), no
// unregistered route can appear, and disabled conditional surfaces
// (native/local) stay out of the document.

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAPIDocGeneratedFromLiveRoutes(t *testing.T) {
	env := newTestEnv(t, false)
	if len(env.hs.apiDocUndoc) > 0 {
		t.Fatalf("registered routes without annotations: %v", env.hs.apiDocUndoc)
	}
	resp := env.call(t, "GET", "/api/v1/openapi.json", "", "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("openapi.json status %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi version %v", doc["openapi"])
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths object missing")
	}
	for _, want := range []string{
		"/healthz",
		"/api/v1/openapi.json",
		"/api/v1/agent/whoami",
		"/api/v1/spaces/{space}/agent/context",
		"/api/v1/spaces/{space}/changes/since",
		"/api/v1/spaces/{space}/documents/{doc}/content",
		"/api/v1/spaces/{space}/attachments/{path}",
	} {
		if _, ok := paths[want]; !ok {
			t.Fatalf("live route missing from document: %s", want)
		}
	}
	if len(paths) < 40 {
		t.Fatalf("document has only %d paths; generation broken", len(paths))
	}
	// conditional surfaces are disabled in this deployment: absent
	for _, gone := range []string{"/api/v1/native/status", "/api/v1/local/vaults"} {
		if _, ok := paths[gone]; ok {
			t.Fatalf("disabled route present in document: %s", gone)
		}
	}
	// annotated routes keep their merged detail
	who, ok := paths["/api/v1/agent/whoami"].(map[string]any)["get"].(map[string]any)
	if !ok || who["summary"] == "" || who["summary"] == nil {
		t.Fatal("annotation merge lost the whoami operation detail")
	}
	if parameters, ok := paths["/api/v1/spaces/{space}/documents/{doc}"].(map[string]any)["parameters"].([]any); !ok || len(parameters) != 2 {
		t.Fatal("shared path parameters missing from generated contract")
	}
	// YAML variant parses and covers the same live route set
	resp2 := env.call(t, "GET", "/api/v1/openapi.yaml", "", "", "")
	if resp2.StatusCode != 200 {
		t.Fatalf("openapi.yaml status %d", resp2.StatusCode)
	}
	var ydoc map[string]any
	if err := yaml.NewDecoder(resp2.Body).Decode(&ydoc); err != nil {
		t.Fatal(err)
	}
	ypaths, ok := ydoc["paths"].(map[string]any)
	if !ok || len(ypaths) != len(paths) {
		t.Fatalf("yaml/json route coverage mismatch: %d vs %d", len(ypaths), len(paths))
	}
}
