package service

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"sync"
	"testing"
)

func TestConcurrentOwnerDemotionsRetainOwner(t *testing.T) {
	h := newHarness(t)
	h.svc.Cfg.AuthMode = "local"
	ctx := context.Background()
	v, err := h.svc.CreateSpace(ctx, h.actor, "Concurrent ownership", "shared")
	if err != nil {
		t.Fatal(err)
	}
	other := w01User(t, h, "other-owner@share.test")
	if err := h.svc.ShareWithUser(ctx, h.actor, v.ID, other.UserID, domain.RoleOwner); err != nil {
		t.Fatal(err)
	}
	first, _ := h.svc.Spaces.Membership(ctx, v.ID, h.actor.UserID)
	second, _ := h.svc.Spaces.Membership(ctx, v.ID, other.UserID)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, entry := range []struct {
		actor *Actor
		id    string
	}{{h.actor, first.ID}, {other, second.ID}} {
		wg.Add(1)
		go func(a *Actor, id string) {
			defer wg.Done()
			<-start
			errs <- h.svc.SetMemberRole(ctx, a, v.ID, id, domain.RoleViewer)
		}(entry.actor, entry.id)
	}
	close(start)
	wg.Wait()
	close(errs)
	successful := 0
	for err := range errs {
		if err == nil {
			successful++
		}
	}
	n, err := h.svc.Spaces.OwnerCount(ctx, v.ID)
	if err != nil || n != 1 || successful != 1 {
		t.Fatalf("owners=%d successful=%d err=%v", n, successful, err)
	}
}
