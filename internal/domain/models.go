package domain

import "time"

// Roles within a space.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	return r == RoleOwner || r == RoleEditor || r == RoleViewer
}

// Action enumerates the authorization-checked operations from the
// Netsidian Permission Matrix.
type Action string

const (
	ActionRead          Action = "read"
	ActionSearch        Action = "search"
	ActionExport        Action = "export"
	ActionCreate        Action = "create"
	ActionEdit          Action = "edit"
	ActionAttachments   Action = "attachments"
	ActionTrash         Action = "trash"
	ActionRestore       Action = "restore"
	ActionPurge         Action = "purge"
	ActionMemoryPropose Action = "memory_propose"
	ActionMemoryReview  Action = "memory_review"
	ActionMemoryDelete  Action = "memory_delete"
	ActionMembers       Action = "members"
	ActionSpaceManage   Action = "space_manage"
)

// RoleCan is the authoritative permission table derived from the
// Netsidian Permission Matrix: viewer reads/searches/exports; editor adds
// create/edit/trash/restore/memory propose+review+delete; owner adds purge,
// membership and space management.
func RoleCan(r Role, a Action) bool {
	if r != RoleOwner && r != RoleEditor && r != RoleViewer {
		return false
	}
	switch a {
	case ActionRead, ActionSearch, ActionExport:
		return true
	case ActionEdit, ActionCreate, ActionAttachments, ActionTrash, ActionMemoryPropose, ActionMemoryReview, ActionMemoryDelete:
		return r == RoleEditor || r == RoleOwner
	case ActionRestore:
		return r == RoleEditor || r == RoleOwner
	case ActionPurge, ActionMembers, ActionSpaceManage:
		return r == RoleOwner
	}
	return false
}

// Scope strings from the P0 API scope table (key-based access).
const (
	ScopeDocumentsRead    = "documents:read"
	ScopeDocumentsWrite   = "documents:write"
	ScopeDocumentsDelete  = "documents:delete"
	ScopeDocumentsPurge   = "documents:purge"
	ScopeAttachmentsRead  = "attachments:read"
	ScopeAttachmentsWrite = "attachments:write"
	ScopeSearchRead       = "search:read"
	ScopeGraphRead        = "graph:read"
	ScopeHistoryRead      = "history:read"
	ScopeHistoryRestore   = "history:restore"
	ScopeExportRead       = "export:read"
	ScopeMemoryRead       = "memory:read"
	ScopeMemoryWrite      = "memory:write"
	ScopeMemoryApprove    = "memory:approve"
	ScopeMemoryDelete     = "memory:delete"
	ScopeEventsRead       = "events:read"
	ScopeMembersManage    = "members:manage"
)

// ValidScope reports whether s is a known P0 scope.
func ValidScope(s string) bool {
	switch s {
	case ScopeDocumentsRead, ScopeDocumentsWrite, ScopeDocumentsDelete, ScopeDocumentsPurge,
		ScopeAttachmentsRead, ScopeAttachmentsWrite, ScopeSearchRead, ScopeGraphRead, ScopeHistoryRead,
		ScopeHistoryRestore, ScopeExportRead, ScopeMemoryRead, ScopeMemoryWrite,
		ScopeMemoryApprove, ScopeMemoryDelete, ScopeEventsRead, ScopeMembersManage:
		return true
	}
	return false
}

// ScopeForAction maps an action to the API key scope that must cover it.
func ScopeForAction(a Action) string {
	switch a {
	case ActionRead:
		return ScopeDocumentsRead
	case ActionSearch:
		return ScopeSearchRead
	case ActionExport:
		return ScopeExportRead
	case ActionCreate, ActionEdit:
		return ScopeDocumentsWrite
	case ActionAttachments:
		return ScopeAttachmentsWrite
	case ActionTrash:
		return ScopeDocumentsDelete
	case ActionRestore:
		return ScopeHistoryRestore
	case ActionPurge:
		return ScopeDocumentsPurge
	case ActionMemoryPropose:
		return ScopeMemoryWrite
	case ActionMemoryReview:
		return ScopeMemoryApprove
	case ActionMemoryDelete:
		return ScopeMemoryDelete
	case ActionMembers:
		return ScopeMembersManage
	case ActionSpaceManage:
		return ScopeMembersManage
	}
	return ""
}

type User struct {
	ID           string
	DisplayName  string
	Email        string
	PasswordHash string // Argon2id encoded; empty for external-only users
	Disabled     bool
	IsOperator   bool
	CreatedAt    time.Time
}

type Space struct {
	ID                string     `json:"id"`
	Slug              string     `json:"slug"`
	Name              string     `json:"name"`
	Kind              string     `json:"kind"` // personal | shared
	StorageBinding    string     `json:"storageBinding"`
	ArchivedAt        *time.Time `json:"archivedAt,omitempty"`
	PermissionVersion int64      `json:"permissionVersion"`
	CreatedAt         time.Time  `json:"createdAt"`
}

type Membership struct {
	ID      string `json:"id"`
	SpaceID string `json:"spaceId"`
	UserID  string `json:"userId"`
	Role    Role   `json:"role"`
}

type Document struct {
	ID              string
	SpaceID         string
	Path            string // canonical relative forward-slash path
	NormKey         string // case-fold collision key within the space
	Kind            string // note | memory | canvas | base | attachment
	CurrentRevision string // revision id; "" never happens after create
	TombstoneAt     *time.Time
	PurgeAfter      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Revision struct {
	ID              string
	DocumentID      string
	Predecessor     string
	SHA256          string
	ByteLength      int64
	Creator         string
	OperationID     string
	HistoryLocation string
	CreatedAt       time.Time
}

// Phase of the durable operation journal.
type OperationPhase string

const (
	OpPhasePending        OperationPhase = "pending"
	OpPhaseStorageWritten OperationPhase = "storage_written"
	OpPhaseCommitted      OperationPhase = "committed"
	OpPhaseAborted        OperationPhase = "aborted"
	OpPhaseConflict       OperationPhase = "conflict"
)

type OperationKind string

const (
	OpKindContent       OperationKind = "content"
	OpKindCreate        OperationKind = "create"
	OpKindRename        OperationKind = "rename"
	OpKindFolderRename  OperationKind = "folder_rename"
	OpKindTrash         OperationKind = "trash"
	OpKindRestore       OperationKind = "restore"
	OpKindMemoryPropose OperationKind = "memory_propose"
)

type Operation struct {
	ID               string
	SpaceID          string
	DocumentID       string
	Principal        string
	Kind             OperationKind
	Phase            OperationPhase
	ExpectedRevision string
	OldRevision      string
	NewSHA256        string
	PendingPath      string
	TargetPath       string
	OriginPath       string // pre-change path for folder renames (recovery direction)
	RequestHash      string
	IdempotencyKey   string
	Error            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Event struct {
	Seq         int64     `json:"seq"`
	SpaceID     string    `json:"spaceId"`
	EventType   string    `json:"eventType"` // document.committed | document.trashed | document.restored | space.members | memory.changed
	DocumentID  string    `json:"documentId,omitempty"`
	RevisionID  string    `json:"revisionId,omitempty"`
	OperationID string    `json:"operationId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}
