package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/pentaqilo/netsidian/internal/domain"
)

type memoryCursor struct {
	Version    int               `json:"v"`
	Space      string            `json:"s"`
	Generation int64             `json:"g"`
	Request    MemoryRecallInput `json:"r"`
}

func (s *Services) prepareMemoryRecall(ctx context.Context, space string, in *MemoryRecallInput) (*MemoryRecallInput, int64, error) {
	if in == nil {
		return nil, 0, domain.Invalid("memory recall input required")
	}
	copy := *in
	in = &copy
	generation, err := s.Docs.MaxEventSeq(ctx, space)
	if err != nil {
		return nil, 0, err
	}
	if in.Cursor == "" {
		return in, generation, nil
	}
	if len(in.Cursor) > 8192 {
		return nil, 0, domain.Invalid("invalid memory continuation")
	}
	original := *in
	original.Cursor = ""
	encoded, _ := json.Marshal(original)
	empty, _ := json.Marshal(MemoryRecallInput{})
	if string(encoded) != string(empty) {
		return nil, 0, domain.Invalid("pass cursor alone; do not change continuation filters or budget")
	}
	data, err := base64.RawURLEncoding.DecodeString(in.Cursor)
	if err != nil {
		return nil, 0, domain.Invalid("invalid memory continuation")
	}
	var cursor memoryCursor
	if json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Space != space || cursor.Request.Cursor != "" || cursor.Request.Offset < 1 {
		return nil, 0, domain.Invalid("invalid memory continuation")
	}
	if cursor.Generation != generation {
		return nil, 0, domain.Conflict("vault changed; restart memory recall instead of mixing evidence snapshots")
	}
	return &cursor.Request, generation, nil
}
func memoryNextCursor(space string, generation int64, in *MemoryRecallInput, out *MemoryRecallResult) string {
	if !out.HasMore {
		return ""
	}
	request := *in
	request.Cursor = ""
	request.Offset = out.NextOffset
	request.Revision = out.IndexRevision
	request.QueryHash = out.QueryHash
	data, _ := json.Marshal(memoryCursor{Version: 1, Space: space, Generation: generation, Request: request})
	return base64.RawURLEncoding.EncodeToString(data)
}
