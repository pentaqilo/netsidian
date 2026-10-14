package httpapi

import (
	"context"
	"encoding/json"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/service"
	"strings"
	"testing"
)

func TestVaultSharingAndScopedPAT(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t, true)
	cookie := e.setup(t, "owner@share.test")
	owner, _ := e.svc.Users.UserByEmail(ctx, "owner@share.test")
	actor := &service.Actor{UserID: owner.ID}
	v, err := e.svc.CreateSpace(ctx, actor, "Shared vault", "shared")
	if err != nil {
		t.Fatal(err)
	}
	member, err := e.svc.SignupAccount(ctx, "member@share.test", "member-password-123", "Federated member")
	if err != nil {
		t.Fatal(err)
	}
	// Provisioned accounts use a minimal directory; no administrator grant required.
	r := e.call(t, "GET", "/api/v1/spaces/"+v.ID+"/sharing/users?q=member", "", cookie, "")
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	var directory struct {
		Users []ports.SharingUser `json:"users"`
	}
	if err := json.NewDecoder(r.Body).Decode(&directory); err != nil || len(directory.Users) != 1 || directory.Users[0].ID != member.ID {
		t.Fatalf("directory %+v %v", directory, err)
	}
	r = e.call(t, "POST", "/api/v1/spaces/"+v.ID+"/members", `{"userId":"`+member.ID+`","role":"viewer"}`, cookie, "")
	if r.StatusCode != 201 {
		t.Fatal(r.StatusCode)
	}

	detailResponse := e.call(t, "GET", "/api/v1/spaces/"+v.ID, "", cookie, "")
	var public struct {
		Members []struct {
			Membership map[string]any `json:"membership"`
		} `json:"members"`
	}
	if err := json.NewDecoder(detailResponse.Body).Decode(&public); err != nil || len(public.Members) != 2 || public.Members[0].Membership["id"] == nil || public.Members[0].Membership["ID"] != nil {
		t.Fatalf("member API shape %+v %v", public, err)
	}

	ma := &service.Actor{UserID: member.ID}
	rec, secret, err := e.svc.CreateKey(ctx, ma, "member agent", []string{v.ID}, []string{domain.ScopeDocumentsRead, domain.ScopeDocumentsWrite, domain.ScopeSearchRead, domain.ScopeGraphRead}, 7)
	if err != nil {
		t.Fatal(err)
	}
	// A write scope cannot elevate a viewer.
	r = e.call(t, "POST", "/api/v1/spaces/"+v.ID+"/documents", `{"path":"Denied.md","content":"no"}`, "", secret)
	if r.StatusCode != 403 {
		t.Fatalf("viewer key write %d", r.StatusCode)
	}
	r = e.call(t, "GET", "/api/v1/spaces/"+v.ID+"/sharing/users?q=owner", "", "", secret)
	if r.StatusCode != 403 {
		t.Fatalf("PAT directory %d", r.StatusCode)
	}
	detail, err := e.svc.GetSpaceDetail(ctx, &service.Actor{UserID: member.ID, Key: rec}, v.ID)
	if err != nil || len(detail.Members) != 0 {
		t.Fatal("PAT exposed membership directory")
	}
	m, _ := e.svc.Spaces.Membership(ctx, v.ID, member.ID)
	r = e.call(t, "PUT", "/api/v1/spaces/"+v.ID+"/members/"+m.ID, `{"role":"editor"}`, cookie, "")
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	r = e.call(t, "POST", "/api/v1/spaces/"+v.ID+"/documents", `{"path":"Agent Memory/Fact.md","content":"# Fact\n---\n#topic"}`, "", secret)
	if r.StatusCode != 201 {
		t.Fatalf("editor key write %d", r.StatusCode)
	}
	r = e.call(t, "GET", "/api/v1/spaces/"+v.ID+"/search?folder=Agent%20Memory&limit=25", "", "", secret)
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	var page struct {
		HasMore bool `json:"hasMore"`
	}
	if err := json.NewDecoder(r.Body).Decode(&page); err != nil || page.HasMore {
		t.Fatalf("one-result last page hasMore=%v %v", page.HasMore, err)
	}
	// Explicit scopes and vaults required for newly issued keys.
	for _, body := range []string{`{"label":"empty","spaceIds":[],"scopes":[]}`, `{"label":"old","spaceIds":["` + v.ID + `"],"scopes":["memory:write"]}`} {
		if r := e.call(t, "POST", "/api/v1/me/keys", body, cookie, ""); r.StatusCode != 400 {
			t.Fatalf("unsafe PAT accepted %d", r.StatusCode)
		}
	}
	om, _ := e.svc.Spaces.Membership(ctx, v.ID, owner.ID)
	r = e.call(t, "PUT", "/api/v1/spaces/"+v.ID+"/members/"+om.ID, `{"role":"viewer"}`, cookie, "")
	if r.StatusCode != 409 {
		t.Fatalf("last owner demotion %d", r.StatusCode)
	}
	r = e.call(t, "DELETE", "/api/v1/spaces/"+v.ID+"/members/"+m.ID, "", cookie, "")
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	r = e.call(t, "GET", "/api/v1/spaces/"+v.ID+"/search?q=fact", "", "", secret)
	if r.StatusCode != 404 && r.StatusCode != 403 {
		t.Fatalf("removed member PAT %d", r.StatusCode)
	}
	// Invitation is not a bearer grant to whoever obtains the URL.
	url, iv, err := e.svc.Invite(ctx, actor, v.ID, member.Email, domain.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AcceptInvitation(ctx, actor, strings.TrimPrefix(url, "/invite?token=")); err == nil {
		t.Fatal("different email accepted invite")
	}
	other, err := e.svc.CreateSpace(ctx, actor, "Other", "shared")
	if err != nil {
		t.Fatal(err)
	}
	r = e.call(t, "DELETE", "/api/v1/spaces/"+other.ID+"/invitations/"+iv.ID, "", cookie, "")
	if r.StatusCode != 404 {
		t.Fatalf("cross-vault revoke %d", r.StatusCode)
	}
	if _, err := e.svc.AcceptInvitation(ctx, ma, strings.TrimPrefix(url, "/invite?token=")); err != nil {
		t.Fatal(err)
	}
}

func TestAgentFolderRetrievalAndPrefixSearch(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t, false)
	e.setup(t, "search@share.test")
	u, _ := e.svc.Users.UserByEmail(ctx, "search@share.test")
	a := &service.Actor{UserID: u.ID}
	spaces, _, _ := e.svc.Spaces.SpacesForUser(ctx, u.ID)
	v := spaces[0].ID
	for _, d := range []struct{ path, text string }{{"Agent Memory/Fact.md", "---\ntags: [work_item]\n---\n# Fact\nZebrawood fact [[Source]]"}, {"Other/Source.md", "---\ntags: [workXitem]\n---\n# Source\nZebrawood source"}} {
		if _, err := e.svc.CreateNote(ctx, a, v, d.path, d.text, "", false); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := e.svc.Search(ctx, a, &ports.SearchQuery{SpaceID: v, Text: "zebra", FolderPrefix: "Agent Memory", Tags: []string{"work_item"}, TagMode: "exact"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("prefix/filters %d %v", len(hits), err)
	}
	result, err := e.svc.AgentContext(ctx, a, v, &service.AgentContextRequest{Folder: "Agent Memory", IncludeGraph: ptrBool(true), MaxBytes: 4096})
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("folder context %+v %v", result, err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), `"memories"`) {
		t.Fatal("special memory response exposed")
	}
	if _, err = e.svc.Search(ctx, a, &ports.SearchQuery{SpaceID: v, Offset: -1}); err == nil {
		t.Fatal("invalid offset accepted")
	}
}
func ptrBool(v bool) *bool { return &v }
