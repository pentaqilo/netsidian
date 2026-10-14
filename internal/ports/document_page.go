package ports

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/domain"
	"time"
)

// Import batches commit new metadata, revision pointers and derived rows together.
// Revision is nil when repairing an existing document's missing index.
type ImportedDocument struct {
	Document *domain.Document
	Revision *domain.Revision
	Index    *IndexData
}
type DocumentImportRepo interface {
	ImportDocumentBatch(context.Context, string, []ImportedDocument) error
}

type DocumentListEntry struct {
	ID              string     `json:"id"`
	Path            string     `json:"path"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Tags            []string   `json:"tags,omitempty"`
	CurrentRevision string     `json:"currentRevision"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	TombstoneAt     *time.Time `json:"tombstoneAt,omitempty"`
}
type DocumentPage struct {
	Documents  []*DocumentListEntry `json:"documents"`
	NextCursor string               `json:"nextCursor,omitempty"`
	Total      int                  `json:"total"`
}
type DocumentPageRepo interface {
	DocumentMetadataPage(context.Context, string, bool, string, int) (*DocumentPage, error)
}
