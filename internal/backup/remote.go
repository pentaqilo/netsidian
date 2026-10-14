package backup

import (
	"context"
	"fmt"

	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/storage/providers"
	"github.com/pentaqilo/netsidian/internal/storage/s3store"
)

// S3RemoteSource builds a RemoteSource for one vault from its provider
// configuration (sealed per-vault profile or the instance S3 config). It
// reuses the instance's own S3 factory, so endpoint allowlists and
// credential rules apply to backups exactly as they do at runtime.
func S3RemoteSource(cfg *ports.StorageConfig, allowedEndpoints, trustedSTS []string, tokenSource providers.IDTokenSource, readLimit int64, spaceID string) (RemoteSource, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage configuration required")
	}
	store, err := providers.S3Factory(nil, allowedEndpoints, trustedSTS, tokenSource, readLimit)(context.Background(), *cfg)
	if err != nil {
		return nil, err
	}
	c, ok := store.(*s3store.Content)
	if !ok {
		return nil, fmt.Errorf("provider did not produce an S3 content store")
	}
	return &s3Remote{c: c, spaceID: spaceID}, nil
}

// S3StoreSource adapts an already-built S3 content store (for example the
// instance's own bucket using the default credential chain) to RemoteSource.
func S3StoreSource(c *s3store.Content, spaceID string) RemoteSource {
	return &s3Remote{c: c, spaceID: spaceID}
}

type s3Remote struct {
	c       *s3store.Content
	spaceID string
}

func (s *s3Remote) Keys(ctx context.Context) ([]string, error) {
	return s.c.ListSpaceKeys(s.spaceID)
}

func (s *s3Remote) Read(ctx context.Context, key string) ([]byte, error) {
	return s.c.GetSpaceObject(s.spaceID, key)
}
