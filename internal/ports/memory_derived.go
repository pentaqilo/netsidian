package ports

import "context"

// Derived metadata is already canonical in Markdown and projected into the
// revision-guarded metadata index by every ordinary storage/indexing path.
// This optional bounded reader avoids scanning all vault documents or bodies.
type DerivedMetadata struct {
	DocumentID      string
	Path            string
	Revision        string
	IndexedRevision string
	Frontmatter     map[string]any
}
type DerivedIndexReader interface {
	DerivedMetadataPage(context.Context, string, string, int) ([]DerivedMetadata, bool, error)
	DerivedMetadata(context.Context, string, string) (*DerivedMetadata, error)
}
