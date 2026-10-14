package s3store

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

// This suite exercises an actual S3 service rather than mocking SDK methods.
// Run with NETSIDIAN_TEST_S3_ENDPOINT and standard AWS test credentials.
func TestS3Integration(t *testing.T) {
	endpoint := os.Getenv("NETSIDIAN_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set NETSIDIAN_TEST_S3_ENDPOINT to run S3 integration")
	}
	ctx := context.Background()
	cfg, e := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if e != nil {
		t.Fatal(e)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(endpoint); o.UsePathStyle = true })
	bucket := "netsidian-test"
	_, e = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	if e != nil && !strings.Contains(e.Error(), "BucketAlreadyOwnedByYou") {
		t.Fatal(e)
	}
	loc, e := local.NewContent(filepath.Join(t.TempDir(), "local"))
	if e != nil {
		t.Fatal(e)
	}
	c, e := New(ctx, bucket, "us-east-1", endpoint, "integration", true, loc)
	if e != nil {
		t.Fatal(e)
	}
	id := "spc_integration"
	if e = c.PurgeSpace(id); e != nil {
		t.Fatal(e)
	}
	defer c.PurgeSpace(id)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(c.WriteAtomic(id, "Ideas/ą test.md", []byte("# First\n")))
	b, e := c.Read(id, "Ideas/ą test.md")
	check(e)
	if string(b) != "# First\n" {
		t.Fatal("read mismatch")
	}
	check(c.WriteAtomic(id, "Ideas/ą test.md", []byte("# Replacement\n")))
	check(c.Move(id, "Ideas/ą test.md", "Ideas/renamed.md"))
	exists, e := c.Exists(id, "Ideas/ą test.md")
	check(e)
	if exists {
		t.Fatal("move left source")
	}
	if _, e = c.Read("spc_other", "Ideas/renamed.md"); !errors.Is(e, fs.ErrNotExist) {
		t.Fatalf("vault isolation: %v", e)
	}
	if e = c.WriteAtomic(id, "../../escape.md", nil); e == nil {
		t.Fatal("traversal accepted")
	}
	check(c.MakeFolder(id, "Empty/Nested"))
	folders, e := c.Folders(id)
	check(e)
	if !contains(folders, "Empty/Nested") || !contains(folders, "Ideas") {
		t.Fatalf("folders: %v", folders)
	}
	check(c.MoveFolder(id, "Ideas", "Archive"))
	b, e = c.Read(id, "Archive/renamed.md")
	check(e)
	if string(b) != "# Replacement\n" {
		t.Fatal("folder move lost content")
	}
	if e = c.MoveFolder(id, "Archive", "Empty"); e == nil {
		t.Fatal("existing folder overwritten")
	}
	check(c.WriteAtomic(id, "Archive/image.png", []byte("attachment")))
	files, e := c.ListFiles(id, "")
	check(e)
	if len(files) != 2 {
		t.Fatalf("files include markers: %v", files)
	}
	check(c.StashFolder(id, "Archive", "ftr_test", []string{"doc_test"}))
	entries, e := c.StashedFolders(id)
	check(e)
	if len(entries) != 1 || entries[0].Path != "Archive" {
		t.Fatalf("stash metadata: %v", entries)
	}
	check(c.WriteAtomic(id, "Archive/renamed.md", []byte("keep newer content")))
	path, conflicts, e := c.UnstashFolder(id, "ftr_test")
	check(e)
	if path != "Archive" || len(conflicts) != 1 {
		t.Fatalf("restore conflicts: %s %v", path, conflicts)
	}
	b, e = c.Read(id, "Archive/renamed.md")
	check(e)
	if string(b) != "keep newer content" {
		t.Fatal("restore overwrote newer content")
	}
	check(c.Delete(id, "Archive/renamed.md"))
	_, conflicts, e = c.UnstashFolder(id, "ftr_test")
	check(e)
	if len(conflicts) != 0 {
		t.Fatal(conflicts)
	}
	b, e = c.Read(id, "Archive/renamed.md")
	check(e)
	if string(b) != "# Replacement\n" {
		t.Fatal("conflicted original was lost")
	}
	// New adapter instance reads persisted bytes and folders without a cache.
	restart, e := New(ctx, bucket, "us-east-1", endpoint, "integration", true, loc)
	check(e)
	b, e = restart.Read(id, "Archive/image.png")
	check(e)
	if string(b) != "attachment" {
		t.Fatal("restart lost attachment")
	}
	entries, e = restart.StashedFolders(id)
	check(e)
	if len(entries) != 0 {
		t.Fatal("restored entry retained")
	}
	check(c.RemoveFolder(id, "Empty"))
	if e = c.RemoveFolder(id, "Archive"); e == nil {
		t.Fatal("nonempty folder removed")
	}
	// Filesystem vaults keep their selected roots even with S3 enabled.
	external := t.TempDir()
	check(c.BindExternal("spc_external", external))
	check(c.WriteAtomic("spc_external", "note.md", []byte("local")))
	b, e = os.ReadFile(filepath.Join(external, "note.md"))
	check(e)
	if string(b) != "local" {
		t.Fatal("external binding not honored")
	}
	check(c.PurgeSpace("spc_external"))
	if _, e = os.Stat(filepath.Join(external, "note.md")); e != nil {
		t.Fatal("purge deleted attached filesystem")
	}
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
