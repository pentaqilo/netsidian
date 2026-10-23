package service

// Guarded consolidation of agent-authored plans. The service does not
// decide what to summarize; it validates a bounded plan, applies each
// step through the ordinary guarded document services, and keeps a
// durable operation journal with deterministic step identifiers so an
// interrupted operation resumes without duplicating writes. Storage is
// not globally transactional, so a multi-step plan is never described
// as atomic: a failed step yields an honest `partial` receipt with the
// exact applied and pending steps plus a recovery reference. Replay of
// identical input resumes or returns the committed receipt; changed
// input under the same operation ID is rejected. User edits made after
// a partial application are never rolled back: resume re-checks each
// target and reports a conflict instead of overwriting them.

const (
	consolidateMaxSources = 16
	consolidateMaxTargets = 1
	consolidateMaxEntries = 32
	consolidateMaxDetail  = 20000
)

// MemoryConsolidateSource pins evidence the plan relies on: index
// entry keys and/or in-vault documents at expected revisions.
type MemoryConsolidateSource struct {
	Key              string `json:"key,omitempty"`
	DocumentID       string `json:"documentId,omitempty"`
	ExpectedRevision string `json:"expectedRevision,omitempty"`
}

// MemoryConsolidateTarget is one topical document edit. Existing
// documents require an expected revision; a new path is created.
type MemoryConsolidateTarget struct {
	DocumentID          string  `json:"documentId,omitempty"`
	Path                string  `json:"path,omitempty"`
	ExpectedRevision    string  `json:"expectedRevision,omitempty"`
	Markdown            *string `json:"markdown"`
	AdoptManagedSection bool    `json:"adoptManagedSection,omitempty"`
}

// MemoryConsolidateEntry changes index entry status or text; supersede
// records which keys the merged claim replaces.
type MemoryConsolidateEntry struct {
	Key        string   `json:"key"`
	Status     string   `json:"status,omitempty"` // superseded|retracted|disputed|active
	Text       string   `json:"text,omitempty"`
	Supersedes []string `json:"supersedes,omitempty"`
}

type MemoryConsolidateInput struct {
	Folder           string                    `json:"folder,omitempty"`
	OperationID      string                    `json:"operationId"`
	ExpectedRevision string                    `json:"expectedRevision,omitempty"`
	DryRun           bool                      `json:"dryRun,omitempty"`
	Sources          []MemoryConsolidateSource `json:"sources,omitempty"`
	Targets          []MemoryConsolidateTarget `json:"targets,omitempty"`
	Entries          []MemoryConsolidateEntry  `json:"entries,omitempty"`
}

// ConsolidationStep is one deterministic journal step.
type ConsolidationStep struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // target|index
	DocumentID string `json:"documentId,omitempty"`
	Path       string `json:"path,omitempty"`
	Status     string `json:"status"` // planned|applied|blocked|failed
	Revision   string `json:"revision,omitempty"`
	Error      string `json:"error,omitempty"`
}

// MemoryConsolidationReceipt is the honest outcome of one plan.
// Outcome is dry-run, applied, partial or unchanged; partial receipts
// carry the journal recovery reference.
type MemoryConsolidationReceipt struct {
	OperationID   string              `json:"operationId"`
	Outcome       string              `json:"outcome"`
	IndexPath     string              `json:"indexPath"`
	IndexRevision string              `json:"indexRevision,omitempty"`
	Steps         []ConsolidationStep `json:"steps"`
	Warnings      []string            `json:"warnings,omitempty"`
	RecoveryNote  string              `json:"recoveryNote,omitempty"`
	Verified      bool                `json:"verified"`
	Evidence      []MemorySourceRef   `json:"evidence,omitempty"`
	Claims        []MemoryClaimRef    `json:"claims,omitempty"`
	RecoveryRef   string              `json:"recoveryRef,omitempty"`
	Code          string              `json:"code,omitempty"`
}

type consolidateJournal struct {
	Version        int                         `json:"version"`
	Plan           *MemoryConsolidateInput     `json:"plan"`
	Steps          []ConsolidationStep         `json:"steps"`
	IndexID        string                      `json:"indexId"`
	IndexRevision  string                      `json:"indexRevision"`
	IndexMarkdown  string                      `json:"indexMarkdown"`
	TargetMarkdown []string                    `json:"targetMarkdown"`
	Evidence       []MemorySourceRef           `json:"evidence"`
	Claims         []MemoryClaimRef            `json:"claims"`
	Warnings       []string                    `json:"warnings,omitempty"`
	Receipt        *MemoryConsolidationReceipt `json:"receipt,omitempty"`
}

func consolidateOpKey(operationID string) string {
	return "memory-consolidate:" + shaHex([]byte(operationID))
}

func consolidateStepID(operationID, kind, target string) string {
	return "memory-consolidate-step:" + shaHex([]byte(operationID)) + ":" + kind + ":" + shaHex([]byte(target))[:16]
}

func utf8Len(s string) int { return len([]rune(s)) }
