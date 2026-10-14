package service

import (
	"context"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// Health reports whether the durable backends respond. It is used by the
// unauthenticated health endpoints; details stay server-side so the route
// discloses nothing beyond ok/unavailable.
func (s *Services) Health(ctx context.Context) error {
	if p, ok := s.Docs.(interface{ Ping(context.Context) error }); ok {
		if err := p.Ping(ctx); err != nil {
			return domain.Unavailable("database unreachable")
		}
	}
	// Existence of a never-created sentinel path: a nil error proves the
	// storage root is statable right now, while an I/O failure means the
	// mounted volume is not serving.
	if _, err := s.Content.Exists("health-probe", ".health-probe"); err != nil {
		return domain.Unavailable("storage unreachable")
	}
	return nil
}
