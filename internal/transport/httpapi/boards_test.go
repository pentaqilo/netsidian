package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPBoardMovesRequireCSRFRevisionAndSourcePath(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "board-review@example.test")
	space := firstSpaceID(t, env, cookie)
	root := "/api/v1/spaces/" + space + "/boards"
	call := func(method, path, body, revision string, csrf bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, env.srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
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
		t.Cleanup(func() { r.Body.Close() })
		return r
	}
	r := call("POST", root, `{"path":"Tasks/Review","title":"Review"}`, "", false)
	if r.StatusCode != 403 {
		t.Fatalf("CSRF: %d", r.StatusCode)
	}
	r = call("POST", root, `{"path":"Tasks/Review","title":"Review"}`, "", true)
	if r.StatusCode != 201 {
		t.Fatalf("create: %d %v", r.StatusCode, decodeBody(t, r))
	}
	r = call("POST", root+"/tasks?path=Tasks%2FReview", `{"columnId":"todo","title":"A"}`, "", true)
	if r.StatusCode != 201 {
		t.Fatalf("task: %d %v", r.StatusCode, decodeBody(t, r))
	}
	task := decodeBody(t, r)
	body, _ := json.Marshal(map[string]any{"docId": task["id"], "columnId": "done", "fromPath": task["path"]})
	r = call("POST", root+"/move?path=Tasks%2FReview", string(body), "", true)
	if r.StatusCode != 428 {
		t.Fatalf("revision required: %d", r.StatusCode)
	}
	r = call("POST", root+"/move?path=Tasks%2FReview", string(body), task["revision"].(string), true)
	if r.StatusCode != 200 {
		t.Fatalf("move: %d %v", r.StatusCode, decodeBody(t, r))
	}
	moved := decodeBody(t, r)
	if moved["id"] != task["id"] || moved["path"] != "Tasks/Review/done/A.md" {
		t.Fatalf("identity/path: %v", moved)
	}
	r = call("POST", root+"/move?path=Tasks%2FReview", string(body), moved["revision"].(string), true)
	if r.StatusCode != 409 {
		t.Fatalf("stale source path: %d", r.StatusCode)
	}
	r = call("GET", root+"/board?path=Tasks%2FReview", "", "", true)
	if r.StatusCode != 200 || r.Header.Get("ETag") == "" {
		t.Fatalf("resolved board: %d", r.StatusCode)
	}
}
