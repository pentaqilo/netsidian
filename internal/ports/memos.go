package ports

import (
	"context"
	"time"
)

type MemoQuery struct {
	SpaceID       string
	Folder        *string
	Tag           string
	Text          string
	From          time.Time
	Before        time.Time
	BeforeCreated time.Time
	BeforeID      string
	Limit         int
}
type MemoEntry struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Title     string    `json:"title"`
	Revision  string    `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Tags      []string  `json:"tags"`
	Preview   string    `json:"preview"`
}
type MemoResult struct {
	Memos      []MemoEntry `json:"memos"`
	Total      int         `json:"total"`
	Tags       []TagCount  `json:"tags"`
	NextCursor string      `json:"nextCursor,omitempty"`
}
type MemoRepository interface {
	ListMemos(context.Context, *MemoQuery) (*MemoResult, error)
}
