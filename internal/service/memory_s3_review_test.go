package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/s3store"
)

// Exercises the real S3 adapter/SDK against an isolated object HTTP fixture.
// This is not a certification of a live AWS/MinIO deployment.
func TestMemoryReviewS3ServicePath(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var mu sync.Mutex
	objects := map[string][]byte{}
	puts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/memory-test" && r.Method == http.MethodHead {
			w.WriteHeader(200)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/memory-test/")
		switch r.Method {
		case http.MethodPut:
			data, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(500)
				return
			}
			objects[key] = data
			puts++
			w.Header().Set("ETag", fmt.Sprintf("\"%x\"", sha256.Sum256(data)))
			w.WriteHeader(200)
		case http.MethodGet, http.MethodHead:
			data, ok := objects[key]
			if !ok {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(404)
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte("<Error><Code>NoSuchKey</Code></Error>"))
				}
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			if r.Method == http.MethodGet {
				_, _ = w.Write(data)
			}
		default:
			w.WriteHeader(405)
		}
	}))
	defer server.Close()
	fallback, err := local.NewContent(filepath.Join(t.TempDir(), "fallback"))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := s3store.NewWithCredentials(ctx, "memory-test", "us-east-1", server.URL, "review", true, fallback, aws.AnonymousCredentials{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	h.svc.Content = remote
	source, err := h.svc.CreateNote(ctx, h.actor, h.space, "Project/Decision.md", "# Evidence\nExact observed result.", "s3-source", false)
	if err != nil {
		t.Fatal(err)
	}
	exact := "Najjaśniejszy Panie — " + strings.Repeat("zażółć gęślą jaźń\n", 40)
	receipt, err := h.svc.RememberMemory(ctx, h.actor, h.space, &MemoryRememberInput{Key: "preference", Text: "Exact wording in detail", Kind: "preference", EvidenceClass: "user-stated", Detail: exact}, "s3-remember")
	if err != nil || !receipt.Verified {
		t.Fatalf("remember: %+v %v", receipt, err)
	}
	recalled, err := h.svc.RecallMemory(ctx, h.actor, h.space, &MemoryRecallInput{Key: "preference"})
	if err != nil || len(recalled.Entries) != 1 {
		t.Fatal(recalled, err)
	}
	detail, err := h.svc.ReadNote(ctx, h.actor, h.space, recalled.Entries[0].Detail.DocumentID)
	if err != nil || detail.Markdown != exact {
		t.Fatal("S3 detail lost exact wording", err)
	}
	body := "Derived result from the decision."
	plan := &MemoryConsolidateInput{OperationID: "s3-consolidate", Sources: []MemoryConsolidateSource{{DocumentID: source.ID, ExpectedRevision: source.CurrentRevision}}, Targets: []MemoryConsolidateTarget{{Path: "Project/Summary.md", Markdown: &body}}}
	applied, err := h.svc.ConsolidateMemory(ctx, h.actor, h.space, plan)
	if err != nil || !applied.Verified {
		t.Fatal(applied, err)
	}
	target, _ := h.svc.Docs.DocumentByNormKey(ctx, h.space, mustNormKey("Project/Summary.md"))
	status, err := h.svc.DerivedStatusWithInput(ctx, h.actor, h.space, MemoryDerivedInput{DocumentID: target.ID})
	if err != nil || len(status.Derived) != 1 || status.Derived[0].Status != "current" {
		t.Fatal(status, err)
	}
	_, err = h.svc.SaveContent(ctx, h.actor, h.space, source.ID, source.CurrentRevision, "Changed source", "s3-source-change")
	if err != nil {
		t.Fatal(err)
	}
	status, err = h.svc.DerivedStatusWithInput(ctx, h.actor, h.space, MemoryDerivedInput{DocumentID: target.ID})
	if err != nil || status.Derived[0].Status != "stale" {
		t.Fatal(status, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if puts < 5 || len(objects) < 4 {
		t.Fatal("fixture did not exercise remote objects", puts, len(objects))
	}
}
