package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// Proposal is a new memory submitted for review.
type Proposal struct {
	Kind          string   `json:"kind"` // preference|fact|decision|glossary
	Title         string   `json:"title"`
	Content       string   `json:"content"`
	Sources       []Source `json:"sources"`
	ExpiresInDays int      `json:"expiresInDays,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"` // 0..1, advisory only
}

type Source struct {
	DocumentID     string `json:"documentId"`
	SourceRevision string `json:"sourceRevision,omitempty"`
	Locator        string `json:"locator,omitempty"` // heading anchor or line range
}

// ProposeMemory creates the backing memory note (kind=memory) and a
// proposed record. Every memory needs at least one in-space source.
func (s *Services) ProposeMemory(ctx context.Context, a *Actor, spaceID string, p *Proposal) (*ports.MemoryRecord, error) {
	return s.ProposeMemoryKey(ctx, a, spaceID, p, "")
}

// ProposeMemoryKey is the governed agent path (agent-memory PLAN.md): an
// Idempotency-Key makes a proposal replay-safe and journal-repairable; a
// proposal interrupted between note and record heals on retry with the
// same key and body (the note can never be orphaned by a crashed attempt
// that replays).
func (s *Services) ProposeMemoryKey(ctx context.Context, a *Actor, spaceID string, p *Proposal, idemKey string) (*ports.MemoryRecord, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMemoryPropose); err != nil {
		return nil, err
	}
	if p.Title == "" || len(p.Content) == 0 {
		return nil, domain.Invalid("memory title and content are required")
	}
	if len(p.Sources) == 0 {
		return nil, domain.Invalid("a memory requires at least one source document")
	}
	switch p.Kind {
	case "preference", "fact", "decision", "glossary":
	default:
		p.Kind = "fact"
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return nil, domain.Invalid("confidence must be between 0 and 1")
	}
	// Provenance validation: every source is a live in-space document and
	// an explicit source revision must belong to that very document.
	for i := range p.Sources {
		src := p.Sources[i].DocumentID
		if src == "" {
			return nil, domain.Invalid("source document required")
		}
		d, err := s.Docs.Document(ctx, src)
		if err != nil {
			return nil, err
		}
		if d == nil || d.SpaceID != spaceID || d.TombstoneAt != nil {
			return nil, domain.Invalid("source " + src + " is not a document in this space")
		}
		if p.Sources[i].SourceRevision != "" {
			rev, err := s.Docs.Revision(ctx, p.Sources[i].SourceRevision)
			if err != nil {
				return nil, err
			}
			if rev == nil || rev.DocumentID != src {
				return nil, domain.Invalid("source revision " + p.Sources[i].SourceRevision + " does not belong to " + src)
			}
		}
	}
	var reqHash string
	var journal *domain.Operation
	if idemKey != "" {
		reqHash = proposalReqHash(spaceID, p)
		op, err := s.Docs.FindOperationByIdempotency(ctx, spaceID, a.UserID, idemKey)
		if err != nil {
			return nil, err
		}
		if op != nil {
			if op.RequestHash != reqHash {
				return nil, domain.Conflict("idempotency key was reused with a different request")
			}
			if op.Phase == domain.OpPhaseCommitted {
				if m, _ := s.Mem.Memory(ctx, op.TargetPath); m != nil {
					return m, nil // replay of a completed proposal
				}
				return nil, domain.Conflict("proposal journal is inconsistent; contact an operator")
			}
			if op.Phase != domain.OpPhasePending {
				return nil, domain.Conflict("a proposal with this key is in an unexpected state")
			}
			// repairable journal: the note may already exist from a crashed
			// attempt; heal below reusing the ORIGINAL memory id (TargetPath)
			journal = op
		}
	}
	memID := domain.NewID("mem")
	now := time.Now()
	if journal == nil && idemKey != "" {
		journal = &domain.Operation{
			ID: domain.NewID("op"), SpaceID: spaceID, Principal: a.UserID,
			Kind: domain.OpKindMemoryPropose, Phase: domain.OpPhasePending,
			TargetPath: memID, IdempotencyKey: idemKey, RequestHash: reqHash,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.Docs.InsertOperation(ctx, journal); err != nil {
			return nil, domain.Unavailable("could not record the proposal operation")
		}
	}
	if journal != nil {
		if m, _ := s.Mem.Memory(ctx, journal.TargetPath); m != nil {
			return m, nil // record already exists; nothing to heal
		}
		memID = journal.TargetPath
	}

	// backing note: on a healed retry the document id was journalled at the
	// latest successful step, so the same note is reused instead of orphaned
	var doc *DocMeta
	var err error
	if journal != nil && journal.DocumentID != "" {
		if d, derr := s.Docs.Document(ctx, journal.DocumentID); derr == nil && d != nil && d.TombstoneAt == nil {
			doc = &DocMeta{ID: d.ID, Path: d.Path, Kind: d.Kind,
				CurrentRevision: d.CurrentRevision, UpdatedAt: d.UpdatedAt}
		}
	}
	if doc == nil {
		path := memoryPath(p.Title)
		data := buildMemoryMarkdown(p)
		noteKey := idemKey
		if noteKey != "" {
			noteKey += "#note"
		}
		doc, err = s.createNoteAs(ctx, a, spaceID, path, data, noteKey, true, domain.ActionMemoryPropose)
		if err != nil {
			if derr, ok := err.(*domain.Error); ok && derr.Code == domain.ErrCodeConflict {
				path = memoryPath(p.Title) + "-" + randomSuffix(len(p.Title)) + ".md"
				doc, err = s.createNoteAs(ctx, a, spaceID, path, data, noteKey, true, domain.ActionMemoryPropose)
			}
			if err != nil {
				return nil, err
			}
		}
		if journal != nil {
			journal.DocumentID = doc.ID
			_ = s.Docs.SetOperationDocument(ctx, journal.ID, doc.ID, now)
		}
	}
	// record-side idempotency guard: a healed retry must not double-insert
	if existing, err := s.Mem.MemoryByDocument(ctx, doc.ID); err == nil && existing != nil {
		if journal != nil && journal.Phase != domain.OpPhaseCommitted {
			_ = s.Docs.SetOperationPhase(ctx, journal.ID, domain.OpPhaseCommitted, "")
		}
		return existing, nil
	}
	mem := &ports.MemoryRecord{
		ID: memID, SpaceID: spaceID, DocumentID: doc.ID,
		Kind: p.Kind, Title: p.Title, Status: "proposed",
		CreatedBy: a.UserID, CreatedAt: now, UpdatedAt: now,
	}
	if p.ExpiresInDays > 0 {
		exp := now.AddDate(0, 0, p.ExpiresInDays)
		mem.ExpiresAt = &exp
	}
	var sources []ports.MemorySource
	for _, src := range p.Sources {
		rev := src.SourceRevision
		if rev == "" {
			if d, err := s.Docs.Document(ctx, src.DocumentID); err == nil && d != nil {
				rev = d.CurrentRevision
			}
		}
		sources = append(sources, ports.MemorySource{
			ID: domain.NewID("src"), MemoryID: mem.ID, SourceDocumentID: src.DocumentID,
			SourceRevision: rev, Locator: src.Locator,
		})
	}
	if err := s.Mem.UpsertMemory(ctx, mem, sources); err != nil {
		return nil, err
	}
	if journal != nil {
		_ = s.Docs.SetOperationPhase(ctx, journal.ID, domain.OpPhaseCommitted, "")
	}
	_ = s.Mem.MemoryAudit(ctx, &ports.AuditEvent{
		ID: domain.NewID("aud"), Actor: a.UserID, Action: "memory.proposed",
		SpaceID: spaceID, Resource: mem.ID, Outcome: "proposed", Corr: a.Corr,
		Details: actorAuditDetails(a, idemKey != ""), CreatedAt: now,
	})
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "memory.changed", DocumentID: doc.ID})
	return mem, nil
}

// actorAuditDetails records agent identity for governance without logging
// content: key id for agent callers, plain user otherwise.
func actorAuditDetails(a *Actor, idempotent bool) string {
	if a.Key != nil {
		if idempotent {
			return `{"actor":"agent","keyId":"` + a.Key.ID + `","idempotent":true}`
		}
		return `{"actor":"agent","keyId":"` + a.Key.ID + `"}`
	}
	return `{"actor":"user"}`
}

func proposalReqHash(spaceID string, p *Proposal) string {
	var b strings.Builder
	b.WriteString("memprop|" + spaceID + "|" + p.Kind + "|" + p.Title + "|" + p.Content)
	for _, s := range p.Sources {
		b.WriteString("|" + s.DocumentID + "@" + s.SourceRevision + "@" + s.Locator)
	}
	b.WriteString(strconv.FormatInt(int64(p.ExpiresInDays), 10))
	return shaHex([]byte(b.String()))
}

func memoryPath(title string) string {
	return "Memory/" + Slugify(title) + ".md"
}

func buildMemoryMarkdown(p *Proposal) string {
	var b strings.Builder
	fm := "---\nmemory: true\nkind: " + p.Kind + "\nstatus: proposed\n---\n\n# " + p.Title + "\n\n"
	if p.Confidence > 0 {
		fm = "---\nmemory: true\nkind: " + p.Kind + "\nstatus: proposed\nconfidence: " +
			strconv.FormatFloat(p.Confidence, 'f', 2, 64) + "\n---\n\n# " + p.Title + "\n\n"
	}
	b.WriteString(fm)
	b.WriteString(p.Content)
	b.WriteString("\n\n## Sources\n\n")
	for _, src := range p.Sources {
		loc := ""
		if src.Locator != "" {
			loc = " (" + src.Locator + ")"
		}
		b.WriteString("- [[" + src.DocumentID + "]]" + loc + "\n")
	}
	return b.String()
}

// MemoryDetail is the review payload: record + sources + audit trail +
// staleness (agent-memory PLAN.md: reviewers see drift before deciding).
type MemoryDetail struct {
	Memory       *ports.MemoryRecord  `json:"memory"`
	Sources      []ports.MemorySource `json:"sources"`
	Audit        []*ports.AuditEvent  `json:"audit,omitempty"`
	Stale        bool                 `json:"stale,omitempty"`
	StaleReasons []string             `json:"staleReasons,omitempty"`
}

func (s *Services) ListMemories(ctx context.Context, a *Actor, spaceID string, statuses []string) ([]*ports.MemoryRecord, error) {
	if _, err := s.requireReadScope(ctx, a, spaceID, domain.ScopeMemoryRead); err != nil {
		return nil, err
	}
	return s.Mem.MemoriesInSpace(ctx, spaceID, statuses)
}

func (s *Services) GetMemory(ctx context.Context, a *Actor, spaceID, memID string) (*MemoryDetail, error) {
	role, err := s.requireReadScope(ctx, a, spaceID, domain.ScopeMemoryRead)
	if err != nil {
		return nil, err
	}
	mem, err := s.Mem.Memory(ctx, memID)
	if err != nil {
		return nil, err
	}
	if mem == nil || mem.SpaceID != spaceID || mem.Status == "deleted" {
		return nil, domain.NotFound("memory not found")
	}
	sources, err := s.Mem.MemorySources(ctx, memID)
	if err != nil {
		return nil, err
	}
	d := &MemoryDetail{Memory: mem, Sources: sources}
	item := s.decorateMemory(ctx, mem)
	d.Stale, d.StaleReasons = item.Stale, item.StaleReasons
	if domain.RoleCan(role, domain.ActionMemoryReview) && (a.Key == nil || len(a.Key.Scopes) == 0 || hasScope(a.Key.Scopes, domain.ScopeMemoryApprove)) {
		d.Audit, err = s.Mem.AuditForMemory(ctx, memID)
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

// ReviewMemory accepts or rejects a proposed memory (memory_review /
// memory:approve). Accepting pins the currently committed revision.
func (s *Services) ReviewMemory(ctx context.Context, a *Actor, spaceID, memID string, accept bool) (*ports.MemoryRecord, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMemoryReview); err != nil {
		return nil, err
	}
	mem, err := s.Mem.Memory(ctx, memID)
	if err != nil {
		return nil, err
	}
	if mem == nil || mem.SpaceID != spaceID || mem.Status != "proposed" {
		return nil, domain.Conflict("only proposed memories can be reviewed")
	}
	status := "rejected"
	pinned := ""
	if accept {
		status = "accepted"
		if d, err := s.Docs.Document(ctx, mem.DocumentID); err == nil && d != nil {
			pinned = d.CurrentRevision
		}
	}
	now := time.Now()
	if err := s.Mem.UpdateMemoryStatus(ctx, memID, status, pinned, a.UserID, now); err != nil {
		return nil, err
	}
	_ = s.Mem.MemoryAudit(ctx, &ports.AuditEvent{
		ID: domain.NewID("aud"), Actor: a.UserID, Action: "memory.review",
		SpaceID: spaceID, Resource: memID, Outcome: status, CreatedAt: now,
	})
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "memory.changed", DocumentID: mem.DocumentID})
	mem.Status = status
	mem.ApprovedRevision = pinned
	return mem, nil
}

// SupersedeMemory marks a memory superseded by another accepted memory.
func (s *Services) SupersedeMemory(ctx context.Context, a *Actor, spaceID, memID, byMemID string) error {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMemoryReview); err != nil {
		return err
	}
	mem, err := s.Mem.Memory(ctx, memID)
	if err != nil {
		return err
	}
	by, err := s.Mem.Memory(ctx, byMemID)
	if err != nil {
		return err
	}
	if mem == nil || mem.SpaceID != spaceID || by == nil || by.SpaceID != spaceID {
		return domain.NotFound("memory not found")
	}
	if mem.Status == "deleted" {
		return domain.Conflict("memory already deleted")
	}
	now := time.Now()
	if err := s.Mem.UpdateMemoryStatus(ctx, memID, "superseded", mem.ApprovedRevision, a.UserID, now); err != nil {
		return err
	}
	if mem2, err := s.Mem.Memory(ctx, memID); err == nil && mem2 != nil {
		mem2.Supersedes = []string{byMemID}
		_ = s.Mem.UpsertMemory(ctx, mem2, nil)
	}
	_ = s.Mem.MemoryAudit(ctx, &ports.AuditEvent{
		ID: domain.NewID("aud"), Actor: a.UserID, Action: "memory.supersede",
		SpaceID: spaceID, Resource: memID, Outcome: byMemID, CreatedAt: now,
	})
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "memory.changed", DocumentID: mem.DocumentID})
	return nil
}

// DeleteMemory marks the record deleted (audit-preserved) and trashes
// its backing note (memory_delete / documents:delete semantics).
func (s *Services) DeleteMemory(ctx context.Context, a *Actor, spaceID, memID string) error {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionMemoryDelete); err != nil {
		return err
	}
	mem, err := s.Mem.Memory(ctx, memID)
	if err != nil {
		return err
	}
	if mem == nil || mem.SpaceID != spaceID || mem.Status == "deleted" {
		return domain.NotFound("memory not found")
	}
	now := time.Now()
	if err := s.Mem.UpdateMemoryStatus(ctx, memID, "deleted", "", a.UserID, now); err != nil {
		return err
	}
	if doc, err := s.Docs.Document(ctx, mem.DocumentID); err == nil && doc != nil && doc.TombstoneAt == nil {
		if err := s.Trash(ctx, a, spaceID, doc.ID, doc.CurrentRevision); err != nil {
			return err
		}
	}
	_ = s.Mem.MemoryAudit(ctx, &ports.AuditEvent{
		ID: domain.NewID("aud"), Actor: a.UserID, Action: "memory.delete",
		SpaceID: spaceID, Resource: memID, Outcome: "deleted", CreatedAt: now,
	})
	s.PublishEvent(&domain.Event{SpaceID: spaceID, EventType: "memory.changed", DocumentID: mem.DocumentID})
	return nil
}

// ExpireMemories is the maintenance pass moving dated memories to expired.
func (s *Services) ExpireMemories(ctx context.Context) int {
	n := 0
	ids, err := s.Spaces.AllSpaceIDs(ctx)
	if err != nil {
		return 0
	}
	for _, sid := range ids {
		mems, err := s.Mem.MemoriesInSpace(ctx, sid, []string{"accepted"})
		if err != nil {
			continue
		}
		for _, m := range mems {
			if m.ExpiresAt != nil && time.Now().After(*m.ExpiresAt) {
				if err := s.Mem.UpdateMemoryStatus(ctx, m.ID, "expired", m.ApprovedRevision, m.ReviewedBy, time.Now()); err == nil {
					n++
					s.PublishEvent(&domain.Event{SpaceID: sid, EventType: "memory.changed", DocumentID: m.DocumentID})
				}
			}
		}
	}
	return n
}
