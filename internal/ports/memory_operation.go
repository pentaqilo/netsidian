package ports

import "context"

// CommitMemoryOperation atomically publishes a verified logical receipt and its
// generation invalidation. It does not make document storage multi-file atomic.
type MemoryOperationCommitter interface {
	CommitMemoryOperation(ctx context.Context, spaceID, operationID, journal string) error
}
