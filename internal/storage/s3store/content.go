// Package s3store stores managed vault content as S3 objects. SQLite,
// the operation journal and immutable revision history remain local.
package s3store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/storage/local"
)

type Content struct {
	client         *s3.Client
	bucket, prefix string
	readLimit      int64
	local          *local.Content
	mu             sync.RWMutex
	localIDs       map[string]bool
}

func New(ctx context.Context, bucket, region, endpoint, prefix string, pathStyle bool, localContent *local.Content) (*Content, error) {
	return NewWithCredentials(ctx, bucket, region, endpoint, prefix, pathStyle, localContent, nil, nil)
}

func NewWithCredentials(ctx context.Context, bucket, region, endpoint, prefix string, pathStyle bool, localContent *local.Content, provider aws.CredentialsProvider, httpClient *http.Client) (*Content, error) {
	if bucket == "" {
		return nil, fmt.Errorf("S3 bucket is required")
	}
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		canon, _, e := domain.NormalizePath(prefix)
		if e != nil || canon != prefix {
			return nil, fmt.Errorf("invalid S3 prefix")
		}
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region), awsconfig.WithHTTPClient(httpClient)}
	if provider != nil {
		options = append(options, awsconfig.WithCredentialsProvider(provider))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	cfg.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	cfg.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = pathStyle
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	if _, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err != nil {
		return nil, fmt.Errorf("S3 bucket unavailable: %w", err)
	}
	return &Content{client: client, bucket: bucket, prefix: prefix, local: localContent, localIDs: map[string]bool{}, readLimit: 64 << 20}, nil
}
func (c *Content) BindManagedLocal(id string) { c.mu.Lock(); c.localIDs[id] = true; c.mu.Unlock() }
func (c *Content) BindExternal(id, root string) error {
	if err := c.local.BindExternal(id, root); err != nil {
		return err
	}
	c.BindManagedLocal(id)
	return nil
}
func (c *Content) UnbindExternal(id string) {
	c.local.UnbindExternal(id)
	c.mu.Lock()
	delete(c.localIDs, id)
	c.mu.Unlock()
}
func (c *Content) isLocal(id string) bool { c.mu.RLock(); defer c.mu.RUnlock(); return c.localIDs[id] }
func (c *Content) root(id string) (string, error) {
	if !local.ValidSpaceID(id) {
		return "", domain.Invalid("invalid space id")
	}
	p := "spaces/" + id + "/"
	if c.prefix != "" {
		p = c.prefix + "/" + p
	}
	return p, nil
}
func (c *Content) key(id, path string) (string, error) {
	root, e := c.root(id)
	if e != nil {
		return "", e
	}
	canon, _, pathErr := domain.NormalizePath(path)
	if pathErr != nil {
		return "", pathErr
	}
	return root + canon, nil
}
func s3Error(err error) error {
	if err == nil {
		return nil
	}
	var a smithy.APIError
	if errors.As(err, &a) {
		switch a.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return fs.ErrNotExist
		}
	}
	return err
}
func (c *Content) put(key string, b []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, e := c.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key), Body: bytes.NewReader(b)})
	return s3Error(e)
}
func (c *Content) get(key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	o, e := c.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if e != nil {
		return nil, s3Error(e)
	}
	defer o.Body.Close()
	limit := c.readLimit
	if limit <= 0 {
		limit = 64 << 20
	}
	if aws.ToInt64(o.ContentLength) > limit {
		return nil, domain.TooLarge("S3 object exceeds the instance read limit")
	}
	b, err := io.ReadAll(io.LimitReader(o.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, domain.TooLarge("S3 object exceeds the instance read limit")
	}
	return b, nil
}
func (c *Content) exists(key string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, e := c.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if errors.Is(s3Error(e), fs.ErrNotExist) {
		return false, nil
	}
	return e == nil, e
}
func (c *Content) remove(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, e := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	return e
}
func (c *Content) copy(from, to string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, e := c.client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(to), CopySource: aws.String(url.PathEscape(c.bucket + "/" + from))})
	return s3Error(e)
}
func (c *Content) objects(prefix string) ([]types.Object, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{Bucket: aws.String(c.bucket), Prefix: aws.String(prefix)})
	out := []types.Object{}
	for p.HasMorePages() {
		page, e := p.NextPage(ctx)
		if e != nil {
			return nil, e
		}
		out = append(out, page.Contents...)
	}
	return out, nil
}

// ListSpaceKeys returns every object key of one vault, relative to the space
// root (the same relative paths WriteAtomic/Read use). Intended for the
// storage-aware backup inventory.
func (c *Content) ListSpaceKeys(id string) ([]string, error) {
	root, e := c.root(id)
	if e != nil {
		return nil, e
	}
	objs, e := c.objects(root)
	if e != nil {
		return nil, e
	}
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		if o.Key == nil {
			continue
		}
		rel := strings.TrimPrefix(*o.Key, root)
		if rel == "" || strings.HasSuffix(rel, "/") {
			continue
		}
		out = append(out, rel)
	}
	return out, nil
}

// GetSpaceObject reads one object by space-relative key.
func (c *Content) GetSpaceObject(id, rel string) ([]byte, error) {
	k, e := c.key(id, rel)
	if e != nil {
		return nil, e
	}
	return c.get(k)
}

func (c *Content) WriteAtomic(id, path string, b []byte) error {
	if c.isLocal(id) {
		return c.local.WriteAtomic(id, path, b)
	}
	k, e := c.key(id, path)
	if e != nil {
		return e
	}
	return c.put(k, b)
}
func (c *Content) Read(id, path string) ([]byte, error) {
	if c.isLocal(id) {
		return c.local.Read(id, path)
	}
	k, e := c.key(id, path)
	if e != nil {
		return nil, e
	}
	return c.get(k)
}
func (c *Content) Exists(id, path string) (bool, error) {
	if c.isLocal(id) {
		return c.local.Exists(id, path)
	}
	k, e := c.key(id, path)
	if e != nil {
		return false, e
	}
	return c.exists(k)
}
func (c *Content) Delete(id, path string) error {
	if c.isLocal(id) {
		return c.local.Delete(id, path)
	}
	k, e := c.key(id, path)
	if e != nil {
		return e
	}
	return c.remove(k)
}
func (c *Content) Move(id, from, to string) error {
	if c.isLocal(id) {
		return c.local.Move(id, from, to)
	}
	src, e := c.key(id, from)
	if e != nil {
		return e
	}
	dst, e := c.key(id, to)
	if e != nil {
		return e
	}
	if e = c.copy(src, dst); e != nil {
		return e
	}
	if e = c.remove(src); e != nil {
		return errors.Join(e, c.remove(dst))
	}
	return nil
}
func (c *Content) PurgeSpace(id string) error {
	if c.isLocal(id) {
		return c.local.PurgeSpace(id)
	}
	root, e := c.root(id)
	if e != nil {
		return e
	}
	objects, e := c.objects(root)
	if e != nil {
		return e
	}
	for _, o := range objects {
		if e = c.remove(aws.ToString(o.Key)); e != nil {
			return e
		}
	}
	return nil
}
func hidden(path string) bool {
	for _, s := range strings.Split(path, "/") {
		if strings.HasPrefix(s, ".") {
			return true
		}
	}
	return false
}
func (c *Content) ListFiles(id, prefix string) ([]ports.FileEntry, error) {
	if c.isLocal(id) {
		return c.local.ListFiles(id, prefix)
	}
	root, e := c.root(id)
	if e != nil {
		return nil, e
	}
	p := root
	if prefix != "" && prefix != "." {
		k, e := c.key(id, prefix)
		if e != nil {
			return nil, e
		}
		p = k + "/"
	}
	objects, e := c.objects(p)
	if e != nil {
		return nil, e
	}
	out := []ports.FileEntry{}
	for _, o := range objects {
		rel := strings.TrimPrefix(aws.ToString(o.Key), root)
		if !hidden(rel) && !strings.HasSuffix(rel, "/") {
			out = append(out, ports.FileEntry{Path: rel, Size: aws.ToInt64(o.Size)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

var _ ports.ContentStore = (*Content)(nil)
var _ ports.AttachmentStore = (*Content)(nil)

// SetReadLimit is applied during provider construction before serving requests.
func (c *Content) SetReadLimit(limit int64) {
	if limit > 0 {
		c.readLimit = limit
	}
}
