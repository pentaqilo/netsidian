package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/pentaqilo/netsidian/internal/service"
)

func TestHTTPAttachmentTrashAndRestore(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "filetrash@x.io")
	space := firstSpaceID(t, env, cookie)
	actorID := decodeBody(t, mustMe(t, env, cookie))["userId"].(string)
	actor := &service.Actor{UserID: actorID, IsOperator: true}
	data := []byte("%PDF-1.4\nattachment trash test\n%%EOF")
	if _, err := env.svc.PutAttachment(context.Background(), actor, space, "Files/file one.pdf", data); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/spaces/" + space + "/attachments/Files/file%20one.pdf"
	// Cookie-authenticated mutation must retain the existing CSRF gate.
	req, err := http.NewRequest("DELETE", env.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	r, err := env.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatalf("missing CSRF guard: %d", r.StatusCode)
	}
	r = env.call(t, "DELETE", path, "", cookie, "")
	if r.StatusCode != 200 {
		t.Fatalf("trash: %d %v", r.StatusCode, decodeBody(t, r))
	}
	entry := decodeBody(t, r)
	if entry["kind"] != "attachment" {
		t.Fatalf("not an attachment trash record: %v", entry)
	}
	if r = env.call(t, "GET", path, "", cookie, ""); r.StatusCode != 404 {
		t.Fatalf("trashed bytes still served: %d", r.StatusCode)
	}
	r = env.call(t, "POST", "/api/v1/spaces/"+space+"/folders/restore", `{"trashId":"`+entry["trashId"].(string)+`"}`, cookie, "")
	if r.StatusCode != 200 {
		t.Fatalf("restore: %d %v", r.StatusCode, decodeBody(t, r))
	}
	if r = env.call(t, "GET", path, "", cookie, ""); r.StatusCode != 200 {
		t.Fatalf("restored bytes not served: %d", r.StatusCode)
	}
}
