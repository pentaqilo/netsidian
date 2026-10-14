package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
	"strings"
	"time"
)

type memoCursor struct {
	Scope   string    `json:"scope"`
	Created time.Time `json:"created"`
	ID      string    `json:"id"`
}

func (s *Services) Memos(ctx context.Context, a *Actor, q *ports.MemoQuery, cursor string) (*ports.MemoResult, error) {
	if _, err := s.Require(ctx, a, q.SpaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	if q.Folder != nil && *q.Folder != "" {
		canon, _, err := domain.NormalizePath(*q.Folder)
		if err != nil {
			return nil, err
		}
		q.Folder = &canon
	}
	q.Tag = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(q.Tag, "#")))
	q.Text = strings.TrimSpace(q.Text)
	if len([]rune(q.Text)) > 400 {
		return nil, domain.Invalid("query exceeds 400 characters")
	}
	if !q.From.IsZero() && !q.Before.IsZero() && !q.Before.After(q.From) {
		return nil, domain.Invalid("date range must end after its start")
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 50 {
		q.Limit = 50
	}
	q.BeforeCreated = time.Time{}
	q.BeforeID = ""
	shape, _ := json.Marshal(q)
	scope := shaHex(shape)
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		var c memoCursor
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Scope != scope || c.Created.IsZero() || c.ID == "" {
			return nil, domain.Invalid("memo cursor does not match these filters")
		}
		q.BeforeCreated = c.Created
		q.BeforeID = c.ID
	}
	repo, ok := s.Index.(ports.MemoRepository)
	if !ok {
		return nil, domain.Unavailable("memo index unavailable")
	}
	result, err := repo.ListMemos(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(result.Memos) > q.Limit {
		result.Memos = result.Memos[:q.Limit]
		last := result.Memos[len(result.Memos)-1]
		raw, _ := json.Marshal(memoCursor{scope, last.CreatedAt, last.ID})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return result, nil
}
