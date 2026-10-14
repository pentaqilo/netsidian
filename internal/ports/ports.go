// Package ports defines the boundaries between shared Go services and
// their adapters (SQL repositories, byte storage, journals). Services
// must not embed dialect or storage assumptions.
package ports

import (
	"context"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// ---------- SQL repositories ----------

type UserRepo interface {
	CreateUser(ctx context.Context, u *domain.User) error
	UserByEmail(ctx context.Context, email string) (*domain.User, error)
	UserByID(ctx context.Context, id string) (*domain.User, error)
	UserCount(ctx context.Context) (int, error)
	SetPassword(ctx context.Context, userID, argonHash string) error
	SolePrincipal(ctx context.Context) (*domain.User, error)

	CreateSession(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time) error
	SessionByTokenHash(ctx context.Context, tokenHash string) (userID string, expiresAt time.Time, ok bool, err error)
	RevokeSession(ctx context.Context, id string) error
	RevokeSessionByHash(ctx context.Context, tokenHash string) error
	RevokeUserSessions(ctx context.Context, userID string) error

	CreateKey(ctx context.Context, k *APIKeyRecord) error
	ListKeys(ctx context.Context, userID string) ([]*APIKeyRecord, error)
	KeyByID(ctx context.Context, id string) (*APIKeyRecord, error)
	KeyBySecretHash(ctx context.Context, hash string) (*APIKeyRecord, error)
	RevokeKey(ctx context.Context, id string) error
	TouchKey(ctx context.Context, id string, at time.Time) error
}

type APIKeyRecord struct {
	ID          string
	Label       string
	PrincipalID string
	SecretHash  string
	Scopes      []string
	SpaceIDs    []string
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

type SpaceRepo interface {
	CreateSpace(ctx context.Context, s *domain.Space) error
	Space(ctx context.Context, id string) (*domain.Space, error)
	SpacesForUser(ctx context.Context, userID string) ([]*domain.Space, []domain.Role, error)
	SlugTaken(ctx context.Context, slug string) (bool, error)
	RenameSpace(ctx context.Context, id, name string) error
	SetStorageBinding(ctx context.Context, id, binding string) error
	ArchiveSpace(ctx context.Context, id string, at *time.Time) error
	DeleteSpace(ctx context.Context, id string) error
	AllSpaceIDs(ctx context.Context) ([]string, error)

	AddMember(ctx context.Context, m *domain.Membership) error
	Members(ctx context.Context, spaceID string) ([]*MemberWithUser, error)
	Membership(ctx context.Context, spaceID, userID string) (*domain.Membership, error)
	SetMemberRole(ctx context.Context, spaceID, memberID string, role domain.Role) error
	RemoveMember(ctx context.Context, spaceID, memberID string) error
	OwnerCount(ctx context.Context, spaceID string) (int, error)
	BumpPermissionVersion(ctx context.Context, spaceID string) (int64, error)
}

type MemberWithUser struct {
	Membership  *domain.Membership `json:"membership"`
	UserID      string             `json:"userId"`
	DisplayName string             `json:"displayName"`
	Email       string             `json:"email,omitempty"`
	Role        domain.Role        `json:"role"`
}

type InvitationRepo interface {
	CreateInvitation(ctx context.Context, inv *Invitation) error
	InvitationsForSpace(ctx context.Context, spaceID string) ([]*Invitation, error)
	InvitationByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error)
	ConsumeInvitation(ctx context.Context, id string, at time.Time) error
	RevokeInvitation(ctx context.Context, id string) error
}

type Invitation struct {
	ID         string      `json:"id"`
	SpaceID    string      `json:"spaceId"`
	Email      string      `json:"email"`
	Role       domain.Role `json:"role"`
	TokenHash  string      `json:"-"`
	IssuedBy   string      `json:"issuedBy"`
	ExpiresAt  time.Time   `json:"expiresAt"`
	ConsumedAt *time.Time  `json:"consumedAt,omitempty"`
	RevokedAt  *time.Time  `json:"revokedAt,omitempty"`
	CreatedAt  time.Time   `json:"createdAt"`
}

type DocumentRepo interface {
	InsertDocument(ctx context.Context, d *domain.Document, rev *domain.Revision) error
	Document(ctx context.Context, id string) (*domain.Document, error)
	DocumentByNormKey(ctx context.Context, spaceID, normKey string) (*domain.Document, error)
	DocumentsInSpace(ctx context.Context, spaceID string, includeTrashed bool) ([]*domain.Document, error)
	Revision(ctx context.Context, id string) (*domain.Revision, error)
	RevisionsForDocument(ctx context.Context, docID string) ([]*domain.Revision, error)
	UpdateDocumentCurrent(ctx context.Context, docID, revID string, updatedAt time.Time) error
	UpdateRevisionHistory(ctx context.Context, revID, location string) error
	RevisionsForOperation(ctx context.Context, opID string) ([]*domain.Revision, error)
	SetDocumentPath(ctx context.Context, docID, path, normKey string, updatedAt time.Time) error
	SetDocumentTrash(ctx context.Context, docID string, tomb, purgeAfter *time.Time) error
	InsertRevision(ctx context.Context, r *domain.Revision) error

	// operations journal
	InsertOperation(ctx context.Context, op *domain.Operation) error
	Operation(ctx context.Context, id string) (*domain.Operation, error)
	SetOperationPhase(ctx context.Context, id string, phase domain.OperationPhase, errMsg string) error
	// SetOperationDocument journals the document id produced by an
	// in-flight operation so a crashed attempt can heal without orphans.
	SetOperationDocument(ctx context.Context, id, docID string, at time.Time) error
	FindOperationByIdempotency(ctx context.Context, spaceID, principal, key string) (*domain.Operation, error)
	OpenOperations(ctx context.Context) ([]*domain.Operation, error)
	DeleteOperation(ctx context.Context, id string) error

	AppendEvent(ctx context.Context, e *domain.Event) (int64, error)
	EventsAfter(ctx context.Context, spaceID string, seq int64, limit int) ([]*domain.Event, error)
	// MaxEventSeq is the durable outbox high-water mark for one space; it
	// doubles as the index generation an agent cursor was minted against.
	MaxEventSeq(ctx context.Context, spaceID string) (int64, error)
}

// AdjEdge is one resolved relationship returned by a bounded
// adjacency scan (agent context and note/selection graph scopes).
type AdjEdge struct {
	SourceID       string `json:"sourceId"`
	TargetID       string `json:"targetId"`
	SourceRevision string `json:"sourceRevision"`
	Fragment       string `json:"fragment,omitempty"`
	Direction      string `json:"direction"` // out | in
}

// IndexRepo covers derived search/link/metadata indexes (rebuildable).
type IndexRepo interface {
	ReplaceDocumentIndex(ctx context.Context, spaceID string, doc *domain.Document, rev string, idx *IndexData) error
	RemoveDocumentIndex(ctx context.Context, docID string) error
	ResolveLinks(ctx context.Context, spaceID string) error
	Search(ctx context.Context, q *SearchQuery) ([]*SearchHit, error)
	Backlinks(ctx context.Context, spaceID, docID string) ([]*Backlink, error)
	Graph(ctx context.Context, spaceID string, limit int) (*GraphData, error)
	LinksForSpace(ctx context.Context, spaceID string) ([]LinkRow, error)
	DocsLite(ctx context.Context, spaceID string) ([]DocLite, error)
	TagsInSpace(ctx context.Context, spaceID string) ([]TagCount, error)
	// Adjacency walks resolved links outward from seed documents up to
	// depth hops, using indexed adjacency lookups with a hard edge cap;
	// it never loads the full edge table.
	Adjacency(ctx context.Context, spaceID string, seeds []string, depth, maxEdges int) ([]AdjEdge, error)
	AllBasenames(ctx context.Context, spaceID string) (map[string]string, error) // basename -> docID (duplicates -> empty value)
	SpaceTitles(ctx context.Context, spaceID string) (map[string]string, error)
	TitleForDocument(ctx context.Context, docID string) (string, error)
}

type IndexData struct {
	Title       string
	Tags        []string
	Headings    []Heading
	Wikilinks   []LinkRef
	BodyPlain   string
	Frontmatter map[string]any
	TasksDone   int
	TasksTotal  int
	Diagnostics []string
}

type Heading struct {
	Level  int    `json:"level"`
	Text   string `json:"text"`
	Anchor string `json:"anchor"`
	Line   int    `json:"line"`
}

type LinkRef struct {
	Target   string `json:"target"` // path or basename (wikilink) or url
	IsURL    bool   `json:"isUrl"`
	Fragment string `json:"fragment,omitempty"`
	Alias    string `json:"alias,omitempty"`
}

type Backlink struct {
	DocumentID string `json:"documentId"`
	Path       string `json:"path"`
	Title      string `json:"title"`
	Snippet    string `json:"snippet"`
}

type GraphNode struct {
	ID     string   `json:"id"`
	Path   string   `json:"path"`
	Title  string   `json:"title"`
	Tags   []string `json:"tags,omitempty"`
	Links  int      `json:"links"`
	Memory bool     `json:"memory,omitempty"`
}

type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type GraphData struct {
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	Total     int         `json:"total"`
	Truncated bool        `json:"truncated"`
}

type SearchQuery struct {
	FolderPrefix string
	TagMode      string // subtree (default) | exact
	Kinds        []string
	ExcludeIDs   []string
	UpdatedAfter time.Time

	SpaceID string
	Text    string
	Tags    []string
	PathHas string
	Limit   int
	Offset  int
}

type SearchHit struct {
	DocumentID string  `json:"documentId"`
	Path       string  `json:"path"`
	Title      string  `json:"title"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
}

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type MemoryRepo interface {
	UpsertMemory(ctx context.Context, m *MemoryRecord, sources []MemorySource) error
	Memory(ctx context.Context, id string) (*MemoryRecord, error)
	MemoriesInSpace(ctx context.Context, spaceID string, statuses []string) ([]*MemoryRecord, error)
	MemoryByDocument(ctx context.Context, docID string) (*MemoryRecord, error)
	MemorySources(ctx context.Context, memoryID string) ([]MemorySource, error)
	UpdateMemoryStatus(ctx context.Context, id, status, approvedRevision, reviewedBy string, updatedAt time.Time) error
	MemoryAudit(ctx context.Context, a *AuditEvent) error
	AuditForMemory(ctx context.Context, memoryID string) ([]*AuditEvent, error)
}

type MemoryRecord struct {
	ID               string     `json:"id"`
	SpaceID          string     `json:"spaceId"`
	DocumentID       string     `json:"documentId"`
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	Status           string     `json:"status"` // proposed|accepted|rejected|superseded|expired|deleted
	ApprovedRevision string     `json:"approvedRevision,omitempty"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	Supersedes       []string   `json:"supersedes,omitempty"`
	CreatedBy        string     `json:"createdBy"`
	ReviewedBy       string     `json:"reviewedBy,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type MemorySource struct {
	ID               string
	MemoryID         string
	SourceDocumentID string
	SourceRevision   string
	Locator          string
}

type AuditEvent struct {
	ID        string
	Actor     string
	Action    string
	SpaceID   string
	Resource  string
	Outcome   string
	Corr      string
	Details   string
	CreatedAt time.Time
}

type PreferenceRepo interface {
	Preferences(ctx context.Context, userID string) (map[string]any, error)
	SetPreferences(ctx context.Context, userID string, prefs map[string]any) error
}

// ---------- Byte storage adapters ----------

// ContentStore stores document bytes under an exclusive per-space root.
// WriteAtomic uses temp file + fsync + rename on the same filesystem.
type ContentStore interface {
	WriteAtomic(spaceID, relPath string, data []byte) error
	Read(spaceID, relPath string) ([]byte, error)
	Exists(spaceID, relPath string) (bool, error)
	Move(spaceID, fromRel, toRel string) error
	Delete(spaceID, relPath string) error
}

// StateStore persists journal and immutable history outside served
// content roots.
type StateStore interface {
	WriteJournal(opID string, data []byte) (string, error)
	SaveHistory(spaceID, docID, revID string, data []byte) (string, error)
	ReadAbsolute(path string) ([]byte, error)
	RemoveAbsolute(path string) error
	HistoryExists(location string) (bool, error)
}

// FolderStore is an optional storage capability for persistent folders:
// creation, rename/move, removal of empty trees and a protected trash
// area for folder-level trash/restore.
type FolderStore interface {
	Folders(spaceID string) ([]string, error)
	MakeFolder(spaceID, path string) error
	// MoveFolder renames a directory inside the space root. The target
	// must not exist; containment is validated like every other path.
	MoveFolder(spaceID, fromRel, toRel string) error
	// RemoveFolder deletes the directory only when it is empty.
	RemoveFolder(spaceID, path string) error
	// StashFolder relocates the directory (and whatever non-document
	// files it still holds) into the protected folder-trash area and
	// records the original path plus the trashed document ids so a
	// later RestoreFolder can merge everything back.
	StashFolder(spaceID, path, trashID string, docIDs []string) error
	// StashedFolders lists folder-trash entries, newest first.
	StashedFolders(spaceID string) ([]FolderTrashEntry, error)
	// UnstashFolder merges the stashed directory back onto the recorded
	// original path. Destination files are never overwritten: any stashed
	// file whose target already exists is left in the stash and returned
	// as a conflict (space-relative path). The trash entry and remaining
	// bytes are removed only when the merge completed with no conflicts,
	// so conflicted originals stay recoverable and retries work.
	UnstashFolder(spaceID, trashID string) (path string, conflicts []string, err error)
}

// FolderTrashEntry describes one trashed folder awaiting restore.
type FolderTrashEntry struct {
	Kind      string   `json:"kind,omitempty"`
	TrashID   string   `json:"trashId"`
	Path      string   `json:"path"`
	TrashedAt string   `json:"trashedAt"`
	DocIDs    []string `json:"docIds"`
}

// AttachmentStore is an optional storage capability that lists non-dot
// files under a prefix, used for attachment collision detection and the
// per-folder attachment view.
type AttachmentStore interface {
	ListFiles(spaceID, prefix string) ([]FileEntry, error)
}

// FileEntry is one non-document file in the space tree.
type FileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}
