package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestEmptyVaultEventStreamOpensImmediately(t *testing.T) {
	env := newTestEnv(t, false)
	cookie := env.setup(t, "stream@example.test")
	u, err := env.svc.Users.UserByEmail(context.Background(), "stream@example.test")
	if err != nil {
		t.Fatal(err)
	}
	spaces, _, err := env.svc.Spaces.SpacesForUser(context.Background(), u.ID)
	if err != nil || len(spaces) != 1 {
		t.Fatalf("personal vault: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", env.srv.URL+"/api/v1/spaces/"+spaces[0].ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	response, err := env.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("event stream did not open before its first event: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("unexpected event stream: %d %s", response.StatusCode, response.Header.Get("Content-Type"))
	}
}
