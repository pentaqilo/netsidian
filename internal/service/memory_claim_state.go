package service

import (
	"context"

	"github.com/pentaqilo/netsidian/internal/domain"
)

type memoryClaimContextKey struct{}
type memoryClaimIndexSnapshot struct {
	revision string
	entries  map[string]*memoryLine
	state    string
}
type memoryClaimInspector struct {
	svc       *Services
	ctx       context.Context
	actor     *Actor
	space     string
	indexes   map[string]*memoryClaimIndexSnapshot
	documents map[string]*domain.Document
	visiting  map[string]bool
	states    map[string]string
	visited   int
	derived   *derivedInspector
}

func memoryClaimContext(ctx context.Context, s *Services, a *Actor, space string) context.Context {
	if existing, ok := ctx.Value(memoryClaimContextKey{}).(*memoryClaimInspector); ok && existing.svc == s && existing.actor == a && existing.space == space {
		return ctx
	}
	inspector := &memoryClaimInspector{svc: s, actor: a, space: space, indexes: map[string]*memoryClaimIndexSnapshot{}, documents: map[string]*domain.Document{}, visiting: map[string]bool{}, states: map[string]string{}}
	ctx = context.WithValue(ctx, memoryClaimContextKey{}, inspector)
	inspector.ctx = ctx
	return ctx
}
func (c *memoryClaimInspector) document(id string) (*domain.Document, error) {
	if d, ok := c.documents[id]; ok {
		return d, nil
	}
	if len(c.documents) >= 256 {
		return nil, domain.TooLarge("claim dependency inspection exceeds 256 documents")
	}
	d, err := c.svc.Docs.Document(c.ctx, id)
	if err != nil {
		return nil, err
	}
	if d != nil && (d.SpaceID != c.space || d.TombstoneAt != nil) {
		d = nil
	}
	c.documents[id] = d
	return d, nil
}
func (c *memoryClaimInspector) index(id string) *memoryClaimIndexSnapshot {
	if snapshot := c.indexes[id]; snapshot != nil {
		return snapshot
	}
	snapshot := &memoryClaimIndexSnapshot{state: "unavailable"}
	if len(c.indexes) >= 16 {
		return snapshot
	}
	c.indexes[id] = snapshot
	d, err := c.document(id)
	if err != nil {
		return snapshot
	}
	if d == nil {
		snapshot.state = "broken"
		return snapshot
	}
	note, err := c.svc.ReadNote(c.ctx, c.actor, c.space, id)
	if err != nil || len(note.Markdown) > 1<<20 {
		return snapshot
	}
	if err := memoryIndexGuard(note.Markdown, ""); err != nil {
		return snapshot
	}
	snapshot.revision = note.Revision
	snapshot.entries = map[string]*memoryLine{}
	for _, entry := range parseMemoryIndex(note.Markdown) {
		snapshot.entries[entry.key] = entry
	}
	snapshot.state = "current"
	return snapshot
}
func (c *memoryClaimInspector) claims(refs []MemoryClaimRef, depth int) string {
	if len(refs) > 16 || depth > 8 {
		return "unavailable"
	}
	state := "current"
	for _, ref := range refs {
		current := c.claim(ref, depth)
		if current == "cyclic" || current == "broken" || current == "unavailable" {
			return current
		}
		if current != "current" {
			state = "stale"
		}
	}
	return state
}
func (c *memoryClaimInspector) claim(ref MemoryClaimRef, depth int) string {
	if ref.VaultID != c.space || ref.IndexDocumentID == "" || ref.Key == "" || ref.IndexRevision == "" {
		return "unavailable"
	}
	identity := ref.IndexDocumentID + ":" + ref.Key + ":" + ref.IndexRevision + ":" + ref.Fingerprint + ":" + ref.ExpectedStatus
	if c.visiting[identity] {
		return "cyclic"
	}
	if state := c.states[identity]; state != "" {
		return state
	}
	c.visited++
	if c.visited > 256 || depth > 8 {
		return "unavailable"
	}
	snapshot := c.index(ref.IndexDocumentID)
	if snapshot.state != "current" {
		return snapshot.state
	}
	entry := snapshot.entries[ref.Key]
	if entry == nil {
		return "broken"
	}
	if ref.Fingerprint == "" && snapshot.revision != ref.IndexRevision || ref.Fingerprint != "" && ref.Fingerprint != claimFingerprint(entry) || memoryStatus(entry.status) != memoryStatus(ref.ExpectedStatus) || entry.status == "retracted" || entry.status == "disputed" {
		return "stale"
	}
	c.visiting[identity] = true
	defer delete(c.visiting, identity)
	state := "current"
	for _, source := range entry.sourceRefs {
		if source.Kind != "document" {
			continue
		}
		d, err := c.document(source.DocumentID)
		if err != nil {
			return "unavailable"
		}
		if d == nil {
			return "broken"
		}
		if source.Revision == "" || source.Revision != d.CurrentRevision {
			state = "stale"
			continue
		}
		if c.derived == nil {
			c.derived, err = c.svc.newDerivedInspector(c.ctx, c.actor, c.space)
			if err != nil {
				return "unavailable"
			}
		}
		nested, err := c.derived.inspect(d.ID, depth+1, nil)
		if err != nil {
			return "unavailable"
		}
		if nested != nil && nested.Status != "current" {
			if nested.Status == "cyclic" {
				return "cyclic"
			}
			state = "stale"
		}
	}
	nested := c.claims(entry.derivedFrom, depth+1)
	if nested != "current" {
		state = nested
	}
	c.states[identity] = state
	return state
}
func (s *Services) boundedMemoryClaimsState(ctx context.Context, a *Actor, space string, refs []MemoryClaimRef) string {
	ctx = memoryClaimContext(ctx, s, a, space)
	return ctx.Value(memoryClaimContextKey{}).(*memoryClaimInspector).claims(refs, 0)
}
