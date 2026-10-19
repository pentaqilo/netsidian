package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPMindMapRevisionAndNodeFileWorkflow(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "mindmap-review@example.test")
	space := firstSpaceID(t, env, cookie)
	root := "/api/v1/spaces/" + space + "/mindmaps"
	call := func(method, url, body, revision string, csrf bool) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, env.srv.URL+url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		req.Header.Set("Content-Type", "application/json")
		if csrf {
			req.Header.Set("X-Netsidian-Requested", "1")
		}
		if revision != "" {
			req.Header.Set("If-Match", `"`+revision+`"`)
		}
		r, err := env.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		var result map[string]any
		if len(data) > 0 {
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("invalid JSON %s", data)
			}
		}
		return r.StatusCode, result
	}
	if status, _ := call("POST", root, `{"path":"Maps/Plan"}`, "", false); status != 403 {
		t.Fatal("CSRF", status)
	}
	status, m := call("POST", root, `{"path":"Maps/Plan","title":"Plan"}`, "", true)
	if status != 201 {
		t.Fatal(status, m)
	}
	status, m = call("POST", root+"/nodes?path=Maps%2FPlan", `{"title":"Central idea","x":0,"y":0}`, "", true)
	if status != 201 {
		t.Fatal(status, m)
	}
	node := m["nodes"].([]any)[0].(map[string]any)
	status, file := call("POST", "/api/v1/spaces/"+space+"/documents", `{"path":"Inbox/Source.md","content":"# Source"}`, "", true)
	if status != 201 {
		t.Fatal(status, file)
	}
	meta := file
	if d, ok := file["document"].(map[string]any); ok {
		meta = d
	}

	linkBody, _ := json.Marshal(map[string]any{"nodeId": node["id"], "docId": meta["id"]})
	linkURL := root + "/nodes/links?path=Maps%2FPlan"
	if status, _ = call("POST", linkURL, string(linkBody), node["revision"].(string), false); status != 403 {
		t.Fatal("link CSRF", status)
	}
	if status, _ = call("POST", linkURL, string(linkBody), "", true); status != 428 {
		t.Fatal("missing node revision", status)
	}
	if status, result := call("POST", linkURL, string(linkBody), node["revision"].(string), true); status != 200 {
		t.Fatal("link", status, result)
	}
	if status, _ = call("POST", linkURL, string(linkBody), node["revision"].(string), true); status != 412 {
		t.Fatal("stale node revision", status)
	}
	body, _ := json.Marshal(map[string]any{"nodeId": node["id"], "docId": meta["id"], "fromPath": meta["path"]})
	if status, _ = call("POST", root+"/nodes/files?path=Maps%2FPlan", string(body), "", true); status != 428 {
		t.Fatal("missing revision", status)
	}
	revision, _ := meta["currentRevision"].(string)
	if revision == "" {
		revision, _ = meta["revision"].(string)
	}
	status, moved := call("POST", root+"/nodes/files?path=Maps%2FPlan", string(body), revision, true)
	if status != 200 {
		t.Fatal(status, moved)
	}
	if status, _ = call("PUT", root+"/config?path=Maps%2FPlan", `{"nodes":[]}`, "", true); status != 428 {
		t.Fatal("missing canvas revision", status)
	}
	status, current := call("GET", root+"/mindmap?path=Maps%2FPlan", "", "", true)
	if status != 200 {
		t.Fatal(status, current)
	}
	rev := current["revision"].(string)
	if status, result := call("PUT", root+"/config?path=Maps%2FPlan", `{"title":"Updated"}`, rev, true); status != 200 {
		t.Fatal(status, result)
	}
	if status, _ = call("PUT", root+"/config?path=Maps%2FPlan", `{"title":"Stale"}`, rev, true); status != 412 {
		t.Fatal("stale revision", status)
	}
}
