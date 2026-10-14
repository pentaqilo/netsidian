package service

import (
	"context"
	"unicode/utf8"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// Search runs the authorized FTS search for one space.
func (s *Services) validateSearch(ctx context.Context, a *Actor, q *ports.SearchQuery) error {
	if _, err := s.Require(ctx, a, q.SpaceID, domain.ActionSearch); err != nil {
		return err
	}
	if utf8.RuneCountInString(q.Text) > 400 || len(q.Tags) > 20 || q.Offset < 0 || q.Offset > 1000000 {
		return domain.Invalid("invalid search query or offset")
	}
	if q.TagMode != "" && q.TagMode != "subtree" && q.TagMode != "exact" {
		return domain.Invalid("tagMode must be exact or subtree")
	}
	if q.Limit <= 0 {
		q.Limit = 25
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	return nil
}
func (s *Services) Search(ctx context.Context, a *Actor, q *ports.SearchQuery) ([]*ports.SearchHit, error) {
	if err := s.validateSearch(ctx, a, q); err != nil {
		return nil, err
	}
	return s.Index.Search(ctx, q)
}

// SearchPage uses one lookahead row, so hasMore never requires a count scan
// and the client doesn't fetch an empty extra page after a full last page.
func (s *Services) SearchPage(ctx context.Context, a *Actor, q *ports.SearchQuery) ([]*ports.SearchHit, bool, error) {
	if err := s.validateSearch(ctx, a, q); err != nil {
		return nil, false, err
	}
	copy := *q
	copy.Limit++
	hits, err := s.Index.Search(ctx, &copy)
	if err != nil {
		return nil, false, err
	}
	more := len(hits) > q.Limit
	if more {
		hits = hits[:q.Limit]
	}
	return hits, more, nil
}

// Backlinks lists notes linking into a document.
func (s *Services) Backlinks(ctx context.Context, a *Actor, spaceID, docID string) ([]*ports.Backlink, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	return s.Index.Backlinks(ctx, spaceID, docID)
}

// Graph returns the link graph (limit clamped; truncated flag set when
// the caller asked for more nodes than returned).
func (s *Services) Graph(ctx context.Context, a *Actor, spaceID string, limit int) (*ports.GraphData, error) {
	if _, err := s.requireReadScope(ctx, a, spaceID, domain.ScopeGraphRead); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 300
	}
	if limit > 1000 {
		limit = 1000
	}
	return s.Index.Graph(ctx, spaceID, limit)
}

// Tags lists tags with counts for filters/palette.
func (s *Services) Tags(ctx context.Context, a *Actor, spaceID string) ([]ports.TagCount, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	return s.Index.TagsInSpace(ctx, spaceID)
}
