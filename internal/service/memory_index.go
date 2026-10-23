package service

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
	"github.com/pentaqilo/netsidian/internal/memorypolicy"
)

// Compact indexed agent memory: a short index document in the ordinary
// agent-memory folder holds one line per entry with a stable key, short
// text, an optional scope and references to existing vault resources.
// Long details live in a linked detail document and are never duplicated
// in the index. The index is a regular Markdown note with normal vault
// permissions, revisions and history; these helpers only parse and merge
// it, every mutation still goes through CreateNote/SaveContent guards.
//
// Entry representation v2 adds typed metadata (kind, scopeId, status,
// validity interval, evidence class, pinned sourceRefs, supersedes and
// derivedFrom claim refs). It is strictly additive: unversioned lines are
// legacy entries whose provenance is unknown, not verified. Unknown
// stored fields are preserved byte-for-byte as raw JSON whenever an
// entry is touched, and entries from a future schema version are read
// but never mutated, so an older binary cannot silently drop metadata
// it does not understand.

const MemoryIndexPath = "agent-memory/index.md"

// MemorySchemaVersion is the entry representation this server writes.
const MemorySchemaVersion = 2

// MemoryPolicyVersion is the shared versioned retention/recall policy
// identity. Generated skills, MCP guidance, UI and docs must reference
// this exact value; the policy text itself has one canonical home and
// is projected from it (root-owned generated guidance reads this hook).
const MemoryPolicyVersion = memorypolicy.Version

const memoryIndexHeader = "---\ntags: [agent-memory]\n---\n\n# Agent memory index\n\n" +
	"Entries are JSON bullets beginning with `- memory: `. Keep keys stable; other text is preserved.\n"

var memoryKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`)

var (
	memoryKinds           = map[string]bool{"fact": true, "preference": true, "decision": true, "episode": true, "procedure": true, "inference": true}
	memoryStatuses        = map[string]bool{"active": true, "superseded": true, "disputed": true, "retracted": true}
	memoryEvidenceClasses = map[string]bool{"user-stated": true, "observed": true, "sourced": true, "inferred": true}
	memorySourceKinds     = map[string]bool{"document": true, "external": true, "interaction": true}
)

func MemoryDetailPath(key string) string { return "agent-memory/details/" + key + ".md" }

// MemoryRef is a resolved reference to a live vault resource.
type MemoryRef struct {
	Target        string `json:"target"`
	Kind          string `json:"kind"` // document|folder|board|map
	DocumentID    string `json:"documentId,omitempty"`
	Path          string `json:"path,omitempty"`
	Revision      string `json:"revision,omitempty"`
	Available     bool   `json:"available"`
	DerivedStatus string `json:"derivedStatus,omitempty"`
}

// MemorySourceRef is a bounded structured evidence reference. In-vault
// document evidence is pinned to a revision and its availability is
// server-verified at record time; external or interaction evidence is
// agent-supplied, never fetched and never marked verified.
type MemorySourceRef struct {
	Target     string `json:"target"`
	Kind       string `json:"kind,omitempty"` // document|external|interaction
	DocumentID string `json:"documentId,omitempty"`
	Path       string `json:"path,omitempty"`
	Revision   string `json:"revision,omitempty"` // pinned; does not follow forward
	Locator    string `json:"locator,omitempty"`
	Quote      string `json:"quote,omitempty"`
	ObservedAt string `json:"observedAt,omitempty"`
	Verified   bool   `json:"verified,omitempty"`
	Available  *bool  `json:"available,omitempty"` // recall-time resolution
	Stale      bool   `json:"stale,omitempty"`     // pinned revision no longer current
}

// MemoryClaimRef identifies a durable claim at a specific index
// revision: destination vault + index document + key + index revision.
// A bare key is never a global identity; the full tuple survives other
// folders or vaults using the same key.
type MemoryClaimRef struct {
	VaultID         string `json:"vaultId,omitempty"`
	IndexDocumentID string `json:"indexDocumentId,omitempty"`
	Key             string `json:"key"`
	IndexRevision   string `json:"indexRevision"`
	Fingerprint     string `json:"fingerprint,omitempty"`
	ExpectedStatus  string `json:"expectedStatus,omitempty"`
}

// MemoryEntry is one decoded index line.
type MemoryEntry struct {
	Key           string            `json:"key"`
	Scope         string            `json:"scope,omitempty"`
	Text          string            `json:"text"`
	Refs          []MemoryRef       `json:"refs,omitempty"`
	Detail        *MemoryRef        `json:"detail,omitempty"`
	SchemaVersion int               `json:"schemaVersion,omitempty"`
	Kind          string            `json:"kind,omitempty"`
	ScopeID       string            `json:"scopeId,omitempty"`
	Status        string            `json:"status,omitempty"`
	EvidenceClass string            `json:"evidenceClass,omitempty"`
	RecordedAt    string            `json:"recordedAt,omitempty"`
	UpdatedAt     string            `json:"updatedAt,omitempty"`
	ValidFrom     string            `json:"validFrom,omitempty"`
	ValidTo       string            `json:"validTo,omitempty"`
	SourceRefs    []MemorySourceRef `json:"sourceRefs,omitempty"`
	Supersedes    []MemoryClaimRef  `json:"supersedes,omitempty"`
	DerivedFrom   []MemoryClaimRef  `json:"derivedFrom,omitempty"`
	DerivedState  string            `json:"derivedState,omitempty"`
}

type memoryLine struct {
	key, scope, text  string
	refs              []string
	detail            string
	schemaVersion     int
	kind, scopeID     string
	status            string
	evidenceClass     string
	recordedAt        string
	updatedAt         string
	validFrom         string
	validTo           string
	reason            string
	sourceRefs        []MemorySourceRef
	supersedes        []MemoryClaimRef
	derivedFrom       []MemoryClaimRef
	originalRaw       map[string]json.RawMessage
	originalCanonical map[string]json.RawMessage
	extra             map[string]json.RawMessage // unknown stored fields, kept losslessly
}

// memoryKnownFields lists every field the parser understands; anything
// else in a stored entry line is preserved verbatim in extra.
var memoryKnownFields = map[string]bool{
	"key": true, "scope": true, "text": true, "refs": true, "detail": true,
	"schemaVersion": true, "kind": true, "scopeId": true, "status": true,
	"evidenceClass": true, "recordedAt": true, "updatedAt": true,
	"validFrom": true, "validTo": true, "sourceRefs": true,
	"supersedes": true, "derivedFrom": true, "reason": true,
}

// parseMemoryLine decodes one entry line. Malformed or invalid entry
// lines are reported as not-an-entry so writers keep the original bytes
// as ordinary text; a future schemaVersion still decodes understood
// fields for reading, but mutation is refused separately.
func parseMemoryLine(line string) (*memoryLine, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "- memory: ") {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(strings.TrimPrefix(trimmed, "- memory: ")), &fields) != nil {
		return nil, false
	}
	l := &memoryLine{}
	str := func(name string) (string, bool) {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			return "", true
		}
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return "", false
		}
		return v, true
	}
	var ok bool
	if l.key, ok = str("key"); !ok {
		return nil, false
	}
	if l.text, ok = str("text"); !ok {
		return nil, false
	}
	if l.scope, ok = str("scope"); !ok {
		return nil, false
	}
	if l.detail, ok = str("detail"); !ok {
		return nil, false
	}
	if raw, has := fields["refs"]; has {
		var refs []string
		if string(raw) != "null" && json.Unmarshal(raw, &refs) != nil {
			return nil, false
		}
		for i, r := range refs {
			refs[i] = strings.TrimSuffix(strings.TrimPrefix(r, "[["), "]]")
		}
		l.refs = refs
	}
	l.detail = strings.TrimSuffix(strings.TrimPrefix(l.detail, "[["), "]]")
	if raw, has := fields["schemaVersion"]; has {
		var v int
		if json.Unmarshal(raw, &v) != nil || v < 0 {
			return nil, false
		}
		l.schemaVersion = v
	}
	for name, dst := range map[string]*string{
		"kind": &l.kind, "scopeId": &l.scopeID, "status": &l.status,
		"evidenceClass": &l.evidenceClass, "recordedAt": &l.recordedAt,
		"updatedAt": &l.updatedAt, "validFrom": &l.validFrom, "validTo": &l.validTo,
		"reason": &l.reason,
	} {
		v, ok := str(name)
		if !ok {
			return nil, false
		}
		*dst = v
	}
	if raw, has := fields["sourceRefs"]; has && string(raw) != "null" {
		if json.Unmarshal(raw, &l.sourceRefs) != nil {
			return nil, false
		}
	}
	for name, dst := range map[string]*[]MemoryClaimRef{"supersedes": &l.supersedes, "derivedFrom": &l.derivedFrom} {
		if raw, has := fields[name]; has && string(raw) != "null" {
			if json.Unmarshal(raw, dst) != nil {
				return nil, false
			}
		}
	}
	for name, raw := range fields {
		if !memoryKnownFields[name] {
			if l.extra == nil {
				l.extra = map[string]json.RawMessage{}
			}
			l.extra[name] = raw
		}
	}
	if !memoryKeyRe.MatchString(l.key) || l.text == "" {
		return nil, false
	}
	canonical := strings.TrimPrefix(l.render(), "- memory: ")
	_ = json.Unmarshal([]byte(canonical), &l.originalCanonical)
	l.originalRaw = fields
	return l, true
}

// parseMemoryIndex extracts entry lines; every other line is data that
// writers must preserve. Malformed entry lines are treated as ordinary
// text so an edited index never loses bytes silently.
func parseMemoryIndex(markdown string) []*memoryLine {
	var out []*memoryLine
	for _, line := range strings.Split(markdown, "\n") {
		if l, ok := parseMemoryLine(line); ok {
			out = append(out, l)
		}
	}
	return out
}

func (l *memoryLine) hasV2() bool {
	return l.schemaVersion > 0 || l.kind != "" || l.scopeID != "" || l.status != "" ||
		l.evidenceClass != "" || l.recordedAt != "" || l.updatedAt != "" ||
		l.validFrom != "" || l.validTo != "" || len(l.sourceRefs) > 0 ||
		len(l.supersedes) > 0 || len(l.derivedFrom) > 0 || len(l.extra) > 0
}

// render serializes known fields in a stable order and re-emits every
// unknown stored field from its raw bytes, so an update of understood
// fields never drops metadata this binary does not understand.
func (l *memoryLine) render() string {
	pairs := make([]string, 0, 16)
	add := func(k string, v any) {
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(v)
		if err != nil {
			return
		}
		if old, ok := l.originalCanonical[k]; ok {
			if raw, ok := l.originalRaw[k]; ok {
				vb = preserveStoredJSON(raw, old, vb)
			}
		}
		pairs = append(pairs, string(kb)+":"+string(vb))
	}
	add("key", l.key)
	if l.scope != "" {
		add("scope", l.scope)
	}
	add("text", l.text)
	if len(l.refs) > 0 {
		bracketed := append([]string(nil), l.refs...)
		for i, r := range bracketed {
			bracketed[i] = "[[" + r + "]]"
		}
		add("refs", bracketed)
	}
	if l.detail != "" {
		add("detail", "[["+l.detail+"]]")
	}
	if l.hasV2() {
		sv := l.schemaVersion
		if sv == 0 {
			sv = MemorySchemaVersion
		}
		add("schemaVersion", sv)
	}
	for _, f := range []struct {
		name string
		v    string
	}{
		{"kind", l.kind}, {"scopeId", l.scopeID}, {"status", l.status},
		{"evidenceClass", l.evidenceClass}, {"recordedAt", l.recordedAt},
		{"updatedAt", l.updatedAt}, {"validFrom", l.validFrom}, {"validTo", l.validTo},
		{"reason", l.reason},
	} {
		if f.v != "" {
			add(f.name, f.v)
		}
	}
	if len(l.sourceRefs) > 0 {
		add("sourceRefs", l.sourceRefs)
	}
	if len(l.supersedes) > 0 {
		add("supersedes", l.supersedes)
	}
	if len(l.derivedFrom) > 0 {
		add("derivedFrom", l.derivedFrom)
	}
	if len(l.extra) > 0 {
		names := make([]string, 0, len(l.extra))
		for name := range l.extra {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			kb, _ := json.Marshal(name)
			pairs = append(pairs, string(kb)+":"+string(l.extra[name]))
		}
	}
	return "- memory: {" + strings.Join(pairs, ",") + "}"
}

// mergeMemoryIndex returns the whole document with one entry replaced or
// appended; unrelated entries and any other content stay byte-identical.
func mergeMemoryIndex(markdown string, l *memoryLine) (string, bool) {
	newLine := l.render()
	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		if parsed, ok := parseMemoryLine(line); ok && parsed.key == l.key {
			if parsed.render() == newLine {
				return markdown, false
			}
			prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			suffix := ""
			if strings.HasSuffix(line, "\r") {
				suffix = "\r"
			}
			lines[i] = prefix + newLine + suffix
			return strings.Join(lines, "\n"), true
		}
	}
	if !strings.HasSuffix(markdown, "\n") && markdown != "" {
		markdown += "\n"
	}
	return markdown + newLine + "\n", true
}

// MemoryRememberInput is one governed index mutation. Omitted metadata
// preserves existing values; explicit clearing happens only through
// documented operations (revise), never by accidentally sending null.
type MemoryRememberInput struct {
	Folder           string            `json:"folder,omitempty"`
	ExpectedRevision string            `json:"expectedRevision,omitempty"`
	Key              string            `json:"key"`
	Text             string            `json:"text"`
	Scope            string            `json:"scope,omitempty"`
	Refs             []string          `json:"refs,omitempty"`
	Detail           string            `json:"detail,omitempty"`
	DetailPath       string            `json:"detailPath,omitempty"`
	DetailRevision   string            `json:"detailRevision,omitempty"`
	Kind             string            `json:"kind,omitempty"`
	ScopeID          string            `json:"scopeId,omitempty"`
	Status           string            `json:"status,omitempty"`
	EvidenceClass    string            `json:"evidenceClass,omitempty"`
	ValidFrom        string            `json:"validFrom,omitempty"`
	ValidTo          string            `json:"validTo,omitempty"`
	SourceRefs       []MemorySourceRef `json:"sourceRefs,omitempty"`
	Supersedes       []MemoryClaimRef  `json:"supersedes,omitempty"`
	DerivedFrom      []MemoryClaimRef  `json:"derivedFrom,omitempty"`
	ClearFields      []string          `json:"clearFields,omitempty"`
}

// MemoryReceipt is the compact confirmation of a remember operation.
type MemoryReceipt struct {
	Key            string `json:"key"`
	Status         string `json:"status"` // created|updated|unchanged
	IndexPath      string `json:"indexPath"`
	IndexRevision  string `json:"indexRevision"`
	DetailPath     string `json:"detailPath,omitempty"`
	DetailRevision string `json:"detailRevision,omitempty"`
	Verified       bool   `json:"verified"`
}

func validateMemoryField(name, v string, max int) error {
	if !utf8.ValidString(v) || strings.ContainsRune(v, 0) || utf8.RuneCountInString(v) > max {
		return domain.Invalid(name + " must be at most " + itoa(max) + " characters without NUL")
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// parseMemoryTime accepts only full RFC 3339 instants and normalizes
// them to UTC; date-only or vague times are rejected rather than
// converted into false precision. Unknown validity stays absent.
func parseMemoryTime(name, v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, domain.Invalid(name + " must be a full RFC 3339 instant with an explicit offset, e.g. 2026-10-22T08:00:00Z")
	}
	return t.UTC(), nil
}

func (s *Services) memoryMu(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.memoryOps == nil {
		s.memoryOps = map[string]*sync.Mutex{}
	}
	if s.memoryOps[key] == nil {
		s.memoryOps[key] = &sync.Mutex{}
	}
	return s.memoryOps[key]
}

func memoryFolder(folder string) (string, error) {
	if folder == "" {
		return "agent-memory", nil
	}
	canon, _, err := domain.NormalizePath(folder)
	if err != nil {
		return "", err
	}
	if err := validFolderRel(canon); err != nil {
		return "", err
	}
	return canon, nil
}

func memoryStepKey(key, step string) string {
	if key == "" {
		return ""
	}
	return "memory-step:" + shaHex([]byte(key)) + ":" + step
}

// resolveMemoryRef resolves a target to a live document (by stable ID
// first, then path) or an existing folder, and reports unavailable
// targets instead of inventing them.
func (s *Services) resolveMemoryRef(ctx context.Context, a *Actor, spaceID, target string) (*MemoryRef, error) {
	ref := &MemoryRef{Target: target}
	if strings.HasPrefix(target, "doc_") {
		if d, err := s.Docs.Document(ctx, target); err != nil {
			return nil, err
		} else if d != nil && d.SpaceID == spaceID && d.TombstoneAt == nil {
			ref.Kind, ref.DocumentID, ref.Path, ref.Revision, ref.Available = "document", d.ID, d.Path, d.CurrentRevision, true
			return ref, nil
		}
		return ref, nil
	}
	canon, key, perr := domain.NormalizePath(target)
	if perr == nil {
		if d, err := s.Docs.DocumentByNormKey(ctx, spaceID, key); err != nil {
			return nil, err
		} else if d != nil && d.TombstoneAt == nil {
			ref.Kind, ref.DocumentID, ref.Path, ref.Revision, ref.Available = "document", d.ID, canon, d.CurrentRevision, true
			return ref, nil
		}
		for _, configName := range []string{boardFileName, mindMapFileName} {
			config, err := s.Docs.DocumentByNormKey(ctx, spaceID, mustNormKey(canon+"/"+configName))
			if err != nil {
				return nil, err
			}
			if config != nil && config.TombstoneAt == nil {
				note, err := s.ReadNote(ctx, a, spaceID, config.ID)
				if err != nil {
					return nil, err
				}
				fm, _, _ := markdown.ParseFrontmatter(note.Markdown)
				_, board := parseBoardConfig(fm)
				_, mindmap := parseMindMapConfig(fm)
				if (configName == boardFileName && board) || (configName == mindMapFileName && mindmap) {
					ref.Kind = "board"
					if mindmap {
						ref.Kind = "map"
					}
					ref.DocumentID = config.ID
					ref.Path = path.Dir(config.Path)
					ref.Revision = config.CurrentRevision
					ref.Available = true
					return ref, nil
				}
			}
		}
		if folders, err := s.ListFolders(ctx, a, spaceID); err != nil {
			return nil, err
		} else {
			for _, f := range folders {
				if f == canon {
					ref.Kind, ref.Path, ref.Available = "folder", canon, true
					return ref, nil
				}
			}
		}
	}
	return ref, nil
}

// validateMemoryMetadata checks the typed v2 fields of a remember
// request. Timestamps are normalized; inverted intervals and unknown
// enum values are rejected at the trust boundary.
func validateMemoryMetadata(in *MemoryRememberInput) (validFrom, validTo string, err error) {
	if in.Kind != "" && !memoryKinds[in.Kind] {
		return "", "", domain.Invalid("kind must be one of fact, preference, decision, episode, procedure or inference")
	}
	if in.Status != "" && !memoryStatuses[in.Status] {
		return "", "", domain.Invalid("status must be one of active, superseded, disputed or retracted")
	}
	if in.EvidenceClass != "" && !memoryEvidenceClasses[in.EvidenceClass] {
		return "", "", domain.Invalid("evidenceClass must be one of user-stated, observed, sourced or inferred")
	}
	if err := validateMemoryField("scopeId", in.ScopeID, 64); err != nil {
		return "", "", err
	}
	for name, value := range map[string]string{"validFrom": in.ValidFrom, "validTo": in.ValidTo} {
		if err := validateMemoryField(name, value, 128); err != nil {
			return "", "", err
		}
	}
	var from, to time.Time
	if in.ValidFrom != "" {
		if from, err = parseMemoryTime("validFrom", in.ValidFrom); err != nil {
			return "", "", err
		}
		validFrom = from.Format(time.RFC3339Nano)
	}
	if in.ValidTo != "" {
		if to, err = parseMemoryTime("validTo", in.ValidTo); err != nil {
			return "", "", err
		}
		validTo = to.Format(time.RFC3339Nano)
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return "", "", domain.Invalid("validFrom must not be after validTo")
	}
	if len(in.DerivedFrom) > 16 {
		return "", "", domain.Invalid("at most 16 derivedFrom references are allowed")
	}
	if len(in.SourceRefs) > 16 {
		return "", "", domain.Invalid("at most 16 sourceRefs are allowed")
	}
	for i := range in.SourceRefs {
		sr := &in.SourceRefs[i]
		if err := validateMemoryField("sourceRef.observedAt", sr.ObservedAt, 128); err != nil {
			return "", "", err
		}
		if err := validateMemoryField("sourceRef.target", sr.Target, 1024); err != nil || sr.Target == "" {
			return "", "", domain.Invalid("sourceRef.target is required and must be at most 1024 characters")
		}
		if sr.Kind != "" && !memorySourceKinds[sr.Kind] {
			return "", "", domain.Invalid("sourceRef.kind must be document, external or interaction")
		}
		if err := validateMemoryField("sourceRef.quote", sr.Quote, 500); err != nil {
			return "", "", err
		}
		if err := validateMemoryField("sourceRef.locator", sr.Locator, 1024); err != nil {
			return "", "", err
		}
		if err := validateMemoryField("sourceRef.revision", sr.Revision, 128); err != nil {
			return "", "", err
		}
	}
	if len(in.Supersedes) > 16 {
		return "", "", domain.Invalid("at most 16 supersedes references are allowed")
	}
	for i := range in.Supersedes {
		if !memoryKeyRe.MatchString(in.Supersedes[i].Key) {
			return "", "", domain.Invalid("supersedes.key must match ^[a-z0-9][a-z0-9._-]{0,95}$")
		}
		if err := validateMemoryField("supersedes.indexRevision", in.Supersedes[i].IndexRevision, 128); err != nil {
			return "", "", err
		}
	}
	return validFrom, validTo, nil
}

// resolveMemorySourceRef pins in-vault document evidence to a revision
// and marks it server-verified; external and interaction evidence is
// recorded as agent-supplied without any claim of server verification.
// The server never fetches URLs or expands arbitrary filesystem paths.
func (s *Services) resolveMemorySourceRef(ctx context.Context, a *Actor, spaceID string, sr *MemorySourceRef) error {
	sr.Available = nil
	sr.Stale = false
	sr.Verified = false
	sr.Path = ""
	if sr.ObservedAt != "" {
		at, err := parseMemoryTime("sourceRef.observedAt", sr.ObservedAt)
		if err != nil {
			return err
		}
		sr.ObservedAt = at.Format(time.RFC3339Nano)
	}
	if sr.Kind == "external" || sr.Kind == "interaction" {
		sr.Verified = false
		sr.DocumentID = ""
		return nil
	}
	target := sr.DocumentID
	if target == "" {
		target = sr.Target
	}
	ref, err := s.resolveMemoryRef(ctx, a, spaceID, target)
	if err != nil {
		return err
	}
	if ref.Available && ref.Kind == "document" {
		sr.Target, sr.DocumentID, sr.Path, sr.Kind = ref.DocumentID, ref.DocumentID, ref.Path, "document"
		if sr.Revision == "" {
			sr.Revision = ref.Revision
		}
		// A pinned revision must actually exist in this document's
		// history; reading a historical body uses the caller's history
		// grant, so verification can never bypass it. A recorded quote
		// must occur in the pinned body: evidence cannot be fabricated.
		if sr.Revision != ref.Revision {
			body, err := s.RevisionContent(ctx, a, spaceID, ref.DocumentID, sr.Revision)
			if err != nil {
				return err
			}
			if sr.Quote != "" && !strings.Contains(body, sr.Quote) {
				return domain.Invalid("sourceRef quote does not occur in the pinned revision body")
			}
		} else if sr.Quote != "" {
			note, err := s.ReadNote(ctx, a, spaceID, ref.DocumentID)
			if err != nil {
				return err
			}
			if !strings.Contains(note.Markdown, sr.Quote) {
				return domain.Invalid("sourceRef quote does not occur in the pinned body")
			}
		}
		sr.Verified = true
		return nil
	}
	if sr.Kind == "document" {
		return domain.Invalid("sourceRef document target must be an existing document in this vault: " + sr.Target)
	}
	// Unresolvable bare targets are agent-supplied evidence (URLs,
	// conversation references); the server records them without
	// fetching and without claiming verification.
	sr.Kind, sr.Verified = "external", false
	return nil
}

// RememberMemory upserts one short entry in the agent-memory index while
// preserving unrelated entries. References must resolve to live vault
// resources; long details go to a linked document. The index write uses
// the revision just read (If-Match semantics) and the persisted result
// is read back before the receipt is returned.
func (s *Services) RememberMemory(ctx context.Context, a *Actor, spaceID string, in *MemoryRememberInput, idemKey string) (*MemoryReceipt, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	if _, err := s.Require(ctx, a, spaceID, domain.ActionEdit); err != nil {
		return nil, err
	}
	if in == nil {
		return nil, domain.Invalid("memory input required")
	}
	if err := validateMemoryField("idempotencyKey", idemKey, 1024); err != nil {
		return nil, err
	}
	in = copyMemoryInput(in)
	folder, err := memoryFolder(in.Folder)
	if err != nil {
		return nil, err
	}
	indexPath := folder + "/index.md"
	lock := s.memoryMu(spaceID + ":" + folder)
	lock.Lock()
	defer lock.Unlock()
	request, _ := json.Marshal(in)
	requestHash := shaHex(request)
	hasIntent, intentErr := s.memoryIntent(ctx, a, spaceID, idemKey, "memory_index", requestHash, false)
	if intentErr != nil {
		return nil, intentErr
	}

	if idemKey != "" {
		op, err := s.Docs.FindOperationByIdempotency(ctx, spaceID, a.UserID, idemKey)
		if err != nil {
			return nil, err
		}
		if op != nil {
			if op.Kind != domain.OperationKind("memory_index") || op.RequestHash != requestHash {
				return nil, domain.Conflict("idempotency key was reused with a different request")
			}
			var receipt MemoryReceipt
			if json.Unmarshal([]byte(op.Error), &receipt) != nil {
				return nil, domain.Conflict("incomplete memory receipt; inspect index before retrying")
			}
			return &receipt, nil
		}
	}
	if !memoryKeyRe.MatchString(in.Key) {
		return nil, domain.Invalid("key must match ^[a-z0-9][a-z0-9._-]{0,95}$")
	}
	if in.Text == "" {
		return nil, domain.Invalid("text is required; put long content in detail, not in the index")
	}
	if err := validateMemoryField("text", in.Text, 500); err != nil {
		return nil, err
	}
	if err := validateMemoryField("scope", in.Scope, 64); err != nil {
		return nil, err
	}
	if len(in.Refs) > 16 {
		return nil, domain.Invalid("at most 16 references are allowed")
	}
	for _, r := range in.Refs {
		if err := validateMemoryField("ref", r, 1024); err != nil {
			return nil, err
		}
	}
	if err := validateMemoryField("detail", in.Detail, 20000); err != nil {
		return nil, err
	}
	validFrom, validTo, err := validateMemoryMetadata(in)
	if err != nil {
		return nil, err
	}

	line := &memoryLine{key: in.Key, scope: in.Scope, text: in.Text}
	receipt := &MemoryReceipt{Key: in.Key, IndexPath: indexPath}
	index, err := s.Docs.DocumentByNormKey(ctx, spaceID, mustNormKey(indexPath))
	if err != nil {
		return nil, err
	}
	if index != nil && index.TombstoneAt != nil {
		return nil, domain.Conflict("memory index is in trash; restore it explicitly")
	}
	var indexNote *NoteContent
	if index != nil {
		indexNote, err = s.ReadNote(ctx, a, spaceID, index.ID)
		if err != nil {
			return nil, err
		}
	}
	if hasIntent {
		recovered, err := s.recoverRememberReceipt(ctx, a, spaceID, idemKey, indexPath, index, indexNote, in, requestHash)
		if err != nil {
			return nil, err
		}
		if recovered != nil {
			return recovered, nil
		}
	}
	if in.ExpectedRevision != "" && (indexNote == nil || indexNote.Revision != in.ExpectedRevision) {
		return nil, domain.Conflict("memory index changed; recall and merge before retrying")
	}

	existingRender := ""
	if indexNote != nil {
		if err := memoryIndexGuard(indexNote.Markdown, in.Key); err != nil {
			return nil, err
		}
		if len(indexNote.Markdown) > 1<<20 {
			return nil, domain.TooLarge("memory index exceeds 1 MiB; split memory into scoped folders")
		}
		found := false
		var existing *memoryLine
		for _, entry := range parseMemoryIndex(indexNote.Markdown) {
			if entry.key != in.Key {
				continue
			}
			if found {
				return nil, domain.Conflict("duplicate memory keys in index; resolve them before updating")
			}
			found = true
			existing = entry
		}
		if existing != nil {
			if existing.schemaVersion > MemorySchemaVersion {
				return nil, domain.Unsupported("memory entry uses a newer schema version; this server refuses to rewrite it")
			}
			// Omitted metadata preserves existing values, including
			// unknown fields kept losslessly in extra.
			*line = *existing
			line.refs = append([]string(nil), existing.refs...)
			line.sourceRefs = append([]MemorySourceRef(nil), existing.sourceRefs...)
			line.supersedes = append([]MemoryClaimRef(nil), existing.supersedes...)
			line.derivedFrom = append([]MemoryClaimRef(nil), existing.derivedFrom...)
			line.extra = map[string]json.RawMessage{}
			for k, v := range existing.extra {
				line.extra[k] = v
			}
			if in.Refs == nil {
				// refs omitted: keep existing links
			} else {
				line.refs = nil
			}
			if in.Text != existing.text {
				line.text = in.Text
			}
			if in.SourceRefs != nil {
				line.sourceRefs = nil
			}
			existingRender = existing.render()
		}
	}
	// Typed metadata applies to new and existing entries alike; an
	// omitted field preserves the existing value on updates.
	if in.Scope != "" {
		line.scope = in.Scope
	}
	if in.Kind != "" {
		line.kind = in.Kind
	}
	if in.ScopeID != "" {
		line.scopeID = in.ScopeID
	}
	if in.Status != "" {
		line.status = in.Status
	}
	if in.EvidenceClass != "" {
		line.evidenceClass = in.EvidenceClass
	}
	if in.ValidFrom != "" {
		line.validFrom = validFrom
	}
	if in.ValidTo != "" {
		line.validTo = validTo
	}
	if in.DerivedFrom != nil {
		line.derivedFrom = nil
	}
	for _, claim := range in.DerivedFrom {
		if err := s.resolveMemoryClaim(ctx, a, spaceID, index, &claim); err != nil {
			return nil, err
		}
		line.derivedFrom = append(line.derivedFrom, claim)
	}
	for _, claim := range in.Supersedes {
		if err := s.resolveMemoryClaim(ctx, a, spaceID, index, &claim); err != nil {
			return nil, err
		}
		line.supersedes = append(line.supersedes, claim)
	}

	for _, target := range in.Refs {
		ref, err := s.resolveMemoryRef(ctx, a, spaceID, target)
		if err != nil {
			return nil, err
		}
		if !ref.Available {
			return nil, domain.Invalid("reference target is not an existing document, folder, board or map in this vault: " + target)
		}
		if ref.DocumentID != "" {
			line.refs = append(line.refs, ref.DocumentID) // stable ID survives renames
		} else {
			line.refs = append(line.refs, ref.Path) // folders keep their path
		}
	}
	for i := range in.SourceRefs {
		if err := s.resolveMemorySourceRef(ctx, a, spaceID, &in.SourceRefs[i]); err != nil {
			return nil, err
		}
		line.sourceRefs = append(line.sourceRefs, in.SourceRefs[i])
	}

	if err := clearMemoryFields(line, in.ClearFields); err != nil {
		return nil, err
	}
	if err := validateMergedMemory(line); err != nil {
		return nil, err
	}

	// Preflight the final index size before a detail can commit. Reserve the
	// longest generated timestamp and a stable document ID, never its full body.
	probe := *line
	if in.Detail != "" && probe.detail == "" {
		probe.detail = "doc_00000000000000000000000000"
	}
	if probe.hasV2() {
		probe.updatedAt = "2026-10-23T00:00:00.123456789Z"
		if probe.recordedAt == "" {
			probe.recordedAt = probe.updatedAt
		}
		if probe.status == "" {
			probe.status = "active"
		}
	}
	projected := memoryIndexHeader + probe.render() + "\n"
	if indexNote != nil {
		projected, _ = mergeMemoryIndex(indexNote.Markdown, &probe)
	}
	if len(projected) > 1<<20 || int64(len(projected)) > s.Cfg.MaxNoteBytes {
		return nil, domain.TooLarge("updated memory index exceeds its capacity; partition scoped folders before writing detail")
	}
	if _, err := s.memoryIntent(ctx, a, spaceID, idemKey, "memory_index", requestHash, true); err != nil {
		return nil, err
	}
	if in.Detail != "" {
		detailPath := in.DetailPath
		if detailPath == "" {
			detailPath = folder + "/details/" + in.Key + ".md"
			if strings.HasPrefix(line.detail, "doc_") {
				previous, err := s.Docs.Document(ctx, line.detail)
				if err != nil {
					return nil, err
				}
				if previous != nil && previous.SpaceID == spaceID && previous.TombstoneAt == nil {
					detailPath = previous.Path
				}
			}
		}
		canon, key, perr := domain.NormalizePath(detailPath)
		if perr != nil {
			return nil, perr
		}
		if !strings.HasPrefix(canon, folder+"/details/") || !strings.HasSuffix(strings.ToLower(canon), ".md") {
			return nil, domain.Invalid("detailPath must be a .md path inside the memory details folder; link existing resources using refs")
		}
		existing, err := s.Docs.DocumentByNormKey(ctx, spaceID, key)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.TombstoneAt != nil {
			return nil, domain.Conflict("detail document is in trash")
		}
		if existing != nil && existing.TombstoneAt == nil {
			if line.detail != canon && line.detail != existing.ID {
				owned := false
				if idemKey != "" {
					op, err := s.Docs.FindOperationByIdempotency(ctx, spaceID, a.UserID, memoryStepKey(idemKey, "detail"))
					if err != nil {
						return nil, err
					}
					owned = op != nil && op.DocumentID == existing.ID && op.RequestHash == createReqHash(spaceID, canon, []byte(in.Detail))
				}
				if !owned {
					return nil, domain.Conflict("detail already exists; use refs to link it instead of overwriting")
				}
			}
			note, err := s.ReadNote(ctx, a, spaceID, existing.ID)
			if err != nil {
				return nil, err
			}
			if note.Markdown != in.Detail {
				if in.DetailRevision == "" {
					return nil, domain.PreconditionRequired("detailRevision required when replacing existing detail content")
				}
				if in.DetailRevision != note.Revision {
					return nil, domain.PreconditionFailed(note.Revision)
				}
				saved, err := s.SaveContent(ctx, a, spaceID, existing.ID, note.Revision, in.Detail, memoryStepKey(idemKey, "detail"), shaHex([]byte(note.Markdown)))
				if err != nil {
					return nil, err
				}
				receipt.DetailRevision = saved.Revision
			} else {
				receipt.DetailRevision = note.Revision
			}
		} else {
			created, err := s.CreateNote(ctx, a, spaceID, canon, in.Detail, memoryStepKey(idemKey, "detail"), false)
			if err != nil {
				return nil, err
			}
			receipt.DetailRevision = created.CurrentRevision
		}
		line.detail = mustDocID(ctx, s, spaceID, canon)
		verifiedDetail, err := s.ReadNote(ctx, a, spaceID, line.detail)
		if err != nil {
			return nil, err
		}
		if verifiedDetail.Markdown != in.Detail {
			return nil, domain.Conflict("detail changed during verification")
		}
		receipt.DetailPath = canon
	}

	// An inference is not verified current knowledge on its own: it
	// must point at supporting evidence the audit trail can follow.
	if line.kind == "inference" && len(line.sourceRefs) == 0 && len(line.refs) == 0 && len(line.derivedFrom) == 0 {
		return nil, domain.Invalid("an inference requires supporting evidence: sourceRefs, refs or derivedFrom")
	}

	// A no-op retry must not advance updatedAt or rewrite bytes.
	dirty := existingRender == "" || line.render() != existingRender
	if dirty && line.hasV2() {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if line.recordedAt == "" {
			line.recordedAt = now
		}
		line.updatedAt = now
		if line.status == "" {
			line.status = "active"
		}
	}

	if index == nil || index.TombstoneAt != nil {
		created, err := s.CreateNote(ctx, a, spaceID, indexPath, memoryIndexHeader+line.render()+"\n", memoryStepKey(idemKey, "index"), false)
		if err != nil {
			return nil, err
		}
		receipt.Status, receipt.IndexRevision = "created", created.CurrentRevision
	} else {
		note := indexNote
		merged, changed := mergeMemoryIndex(note.Markdown, line)
		if len(merged) > 1<<20 {
			return nil, domain.TooLarge("memory index would exceed 1 MiB; partition scoped folders")
		}
		if !changed {
			receipt.Status, receipt.IndexRevision = "unchanged", note.Revision
		} else {
			saved, err := s.SaveContent(ctx, a, spaceID, index.ID, note.Revision, merged, memoryStepKey(idemKey, "index"), shaHex([]byte(note.Markdown)))
			if err != nil {
				return nil, err
			}
			receipt.Status, receipt.IndexRevision = "updated", saved.Revision
		}
	}

	// Read-back verification: the receipt is only honest if the persisted
	// index actually contains the entry with the same key and text.
	note, err := s.ReadNote(ctx, a, spaceID, mustDocID(ctx, s, spaceID, indexPath))
	if err != nil {
		return nil, err
	}
	verified := false
	for _, parsed := range parseMemoryIndex(note.Markdown) {
		if parsed.render() == line.render() {
			verified = true
		}
	}
	if !verified {
		return nil, domain.Unavailable("memory index write could not be verified; inspect " + indexPath + " before retrying")
	}
	receipt.Verified = true
	if idemKey != "" {
		data, _ := json.Marshal(receipt)
		now := time.Now()
		op := &domain.Operation{ID: domain.NewID("op"), SpaceID: spaceID, Principal: a.UserID, Kind: domain.OperationKind("memory_index"), Phase: domain.OpPhaseCommitted, RequestHash: requestHash, IdempotencyKey: idemKey, Error: string(data), CreatedAt: now, UpdatedAt: now}
		if err := s.Docs.InsertOperation(ctx, op); err != nil {
			return nil, domain.Unavailable("saved memory but receipt could not be recorded; retry with the same key")
		}
	}
	return receipt, nil
}

// recoverRememberReceipt rebuilds the receipt of a completed remember
// whose final journal insert was lost. It requires proof that the
// committed index write is this request's own child: the deterministic
// step key, the exact bytes now stored, and the save pinned to this
// request's expected revision.
func (s *Services) recoverRememberReceipt(ctx context.Context, a *Actor, space, idem, indexPath string, index *domain.Document, note *NoteContent, in *MemoryRememberInput, hash string) (*MemoryReceipt, error) {
	if index == nil || note == nil {
		return nil, nil
	}
	produced, err := s.memoryOwnedRevision(ctx, a, space, idem, index.ID, in.ExpectedRevision, note.Markdown)
	if err != nil {
		return nil, err
	}
	if produced == "" {
		return nil, nil
	}
	if produced != note.Revision {
		return nil, domain.Conflict("index was edited after the committed memory step; preserve that edit")
	}
	if err := memoryIndexGuard(note.Markdown, in.Key); err != nil {
		return nil, err
	}
	var entry *memoryLine
	for _, line := range parseMemoryIndex(note.Markdown) {
		if line.key == in.Key {
			entry = line
		}
	}
	if entry == nil || entry.text != in.Text {
		return nil, domain.Conflict("saved memory child differs from the bound request")
	}
	receipt := &MemoryReceipt{Key: in.Key, Status: "updated", IndexPath: indexPath, IndexRevision: produced, Verified: true}
	child, _ := s.Docs.FindOperationByIdempotency(ctx, space, a.UserID, memoryStepKey(idem, "index"))
	if child != nil && child.Kind == domain.OpKindCreate {
		receipt.Status = "created"
	}
	if in.Detail != "" {
		if entry.detail == "" {
			return nil, domain.Unavailable("saved index lacks its exact detail link")
		}
		detail, err := s.ReadNote(ctx, a, space, entry.detail)
		if err != nil {
			return nil, err
		}
		if detail.Markdown != in.Detail {
			return nil, domain.Conflict("detail changed after the committed memory step")
		}
		receipt.DetailPath, receipt.DetailRevision = detail.Path, detail.Revision
	}
	if err := s.saveMemoryReceipt(ctx, a, space, idem, "memory_index", hash, receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func mustNormKey(p string) string {
	_, key, err := domain.NormalizePath(p)
	if err != nil {
		return p
	}
	return key
}

func mustDocID(ctx context.Context, s *Services, spaceID, p string) string {
	d, err := s.Docs.DocumentByNormKey(ctx, spaceID, mustNormKey(p))
	if err != nil || d == nil {
		return ""
	}
	return d.ID
}

// MemoryRecallInput filters the index without loading linked bodies.
// Filters are part of the continuation identity: a later page must
// present the same queryHash and index revision, never a mix of
// snapshots or reinterpreted filters.
type MemoryRecallInput struct {
	Cursor    string `json:"cursor,omitempty"`
	Folder    string `json:"folder,omitempty"`
	Revision  string `json:"revision,omitempty"`
	QueryHash string `json:"queryHash,omitempty"`
	Query     string `json:"query,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Key       string `json:"key,omitempty"`
	Kind      string `json:"kind,omitempty"`
	ScopeID   string `json:"scopeId,omitempty"`
	Status    string `json:"status,omitempty"`
	ValidOn   string `json:"validOn,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
	MaxBytes  int    `json:"maxBytes,omitempty"`
}

// MemoryRecallResult is bounded: entries past the limit or byte budget
// are reported through hasMore/nextOffset, never silently dropped.
type MemoryRecallResult struct {
	IndexPath     string        `json:"indexPath"`
	IndexRevision string        `json:"indexRevision,omitempty"`
	QueryHash     string        `json:"queryHash,omitempty"`
	Entries       []MemoryEntry `json:"entries"`
	Matched       int           `json:"matched"`
	Offset        int           `json:"offset"`
	HasMore       bool          `json:"hasMore"`
	NextOffset    int           `json:"nextOffset,omitempty"`
	NextCursor    string        `json:"nextCursor,omitempty"`
	Truncated     bool          `json:"truncated,omitempty"`
	Note          string        `json:"note,omitempty"`
	Diagnostics   []string      `json:"diagnostics,omitempty"`
}

// recallQueryHash pins continuation to the complete filter
// configuration, not just the index revision.
func recallQueryHash(in *MemoryRecallInput) string {
	b, _ := json.Marshal(struct {
		Folder, Query, Scope, Key, Kind, ScopeID, Status, ValidOn string
		Limit, MaxBytes                                           int
	}{in.Folder, in.Query, in.Scope, in.Key, in.Kind, in.ScopeID, in.Status, in.ValidOn, in.Limit, in.MaxBytes})
	return shaHex(b)[:16]
}

func (s *Services) recallRef(ctx context.Context, a *Actor, spaceID, target string) MemoryRef {
	ref, err := s.resolveMemoryRef(ctx, a, spaceID, target)
	if err != nil || ref == nil {
		return MemoryRef{Target: target, Kind: "unknown"}
	}
	return *ref
}

// recallSourceRef reports evidence state conservatively at read time:
// a missing or trashed source lowers auditability but is not proof the
// claim is false, and a pinned revision never silently follows forward.
func (s *Services) recallSourceRef(ctx context.Context, a *Actor, spaceID string, sr MemorySourceRef) MemorySourceRef {
	out := sr
	if sr.Kind == "external" || sr.Kind == "interaction" {
		out.Verified = false
		out.Available = nil
		out.Stale = false
		out.DocumentID = ""
		out.Path = ""
		return out
	}
	if sr.Kind == "document" || sr.DocumentID != "" {
		available, stale := false, false
		if d, err := s.Docs.Document(ctx, sr.DocumentID); err == nil && d != nil && d.SpaceID == spaceID && d.TombstoneAt == nil {
			available = true
			out.Path = d.Path
			if sr.Revision != "" && sr.Revision != d.CurrentRevision {
				stale = true
			}
		}
		if !available {
			out.Verified = false
		}
		out.Available, out.Stale = &available, stale
	}
	return out
}

// RecallMemory returns filtered index entries and their resolved links
// only; linked document bodies are never included.
func (s *Services) RecallMemory(ctx context.Context, a *Actor, spaceID string, in *MemoryRecallInput) (*MemoryRecallResult, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
	}
	ctx = memoryClaimContext(ctx, s, a, spaceID)
	in, generation, err := s.prepareMemoryRecall(ctx, spaceID, in)
	if err != nil {
		return nil, err
	}
	for name, field := range map[string]struct {
		value string
		max   int
	}{"query": {in.Query, 500}, "scope": {in.Scope, 64}, "key": {in.Key, 96}, "scopeId": {in.ScopeID, 64}, "revision": {in.Revision, 128}, "queryHash": {in.QueryHash, 64}} {
		if err := validateMemoryField(name, field.value, field.max); err != nil {
			return nil, err
		}
	}
	folder, err := memoryFolder(in.Folder)
	if err != nil {
		return nil, err
	}
	indexPath := folder + "/index.md"
	if in.Limit < 0 || in.Limit > 50 || in.MaxBytes < 0 || (in.MaxBytes > 0 && in.MaxBytes < 1024) || in.MaxBytes > 32768 {
		return nil, domain.Invalid("limit must be 1..50 and maxBytes 1024..32768")
	}
	if in.Offset > 0 && in.Revision == "" {
		return nil, domain.Invalid("revision from the first recall page is required with offset")
	}
	if in.Kind != "" && !memoryKinds[in.Kind] {
		return nil, domain.Invalid("kind filter must be one of fact, preference, decision, episode, procedure or inference")
	}
	if in.Status != "" && in.Status != "all" && !memoryStatuses[in.Status] {
		return nil, domain.Invalid("status filter must be active, superseded, disputed, retracted or all")
	}
	var validOnTime time.Time
	if in.ValidOn != "" {
		if validOnTime, err = parseMemoryTime("validOn", in.ValidOn); err != nil {
			return nil, err
		}
	}
	if in.Limit <= 0 {
		in.Limit = 20
	}
	if in.Limit > 50 {
		in.Limit = 50
	}
	if in.MaxBytes <= 0 {
		in.MaxBytes = 8192
	}
	if in.MaxBytes > 32768 {
		in.MaxBytes = 32768
	}
	if in.Offset < 0 {
		return nil, domain.Invalid("offset must not be negative")
	}
	qh := recallQueryHash(in)
	if in.Offset > 0 {
		if in.QueryHash == "" && (in.Kind != "" || in.ScopeID != "" || in.Status != "" || in.ValidOn != "") {
			return nil, domain.Invalid("queryHash from the first recall page is required with typed filters and offset")
		}
		if in.QueryHash != "" && in.QueryHash != qh {
			return nil, domain.Conflict("continuation query differs from the first page; restart recall instead of mixing snapshots")
		}
	}
	out := &MemoryRecallResult{IndexPath: indexPath, QueryHash: qh, Entries: []MemoryEntry{}, Offset: in.Offset}
	index, err := s.Docs.DocumentByNormKey(ctx, spaceID, mustNormKey(indexPath))
	if err != nil {
		return nil, err
	}
	if index == nil || index.TombstoneAt != nil {
		if in.Revision != "" {
			return nil, domain.Conflict("memory index is no longer available; restart recall")
		}
		out.Note = "no memory index yet"
		if len(encodeMemoryEnvelope(out)) > in.MaxBytes {
			return nil, domain.TooLarge("memory metadata exceeds maxBytes")
		}
		return out, nil
	}
	note, err := s.ReadNote(ctx, a, spaceID, index.ID)
	if err != nil {
		return nil, err
	}
	out.IndexRevision = note.Revision
	if len(note.Markdown) > 1<<20 {
		return nil, domain.TooLarge("memory index exceeds 1 MiB; split memory into scoped folders")
	}
	out.Diagnostics = memoryIndexDiagnostics(note.Markdown)
	if in.Offset > 0 && in.QueryHash == "" {
		out.Note = "Legacy continuation binds revision only; return queryHash to bind all filters"
	}
	if in.Revision != "" && in.Revision != note.Revision {
		return nil, domain.Conflict("memory index changed; restart recall")
	}
	if len(note.Markdown) > 1<<20 {
		return nil, domain.TooLarge("memory index exceeds 1 MiB; split memory into scoped folders")
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	scopeWant := strings.ToLower(strings.TrimSpace(in.Scope))
	statusWant := in.Status
	var matched []*memoryLine
	for _, l := range parseMemoryIndex(note.Markdown) {
		if in.Key != "" && l.key != in.Key {
			continue
		}
		if scopeWant != "" && strings.ToLower(l.scope) != scopeWant {
			continue
		}
		if in.Kind != "" && l.kind != in.Kind {
			continue
		}
		if in.ScopeID != "" && l.scopeID != in.ScopeID {
			continue
		}
		// Legacy entries carry no status: they are current knowledge
		// unless explicitly marked otherwise. "all" restores full
		// historical discoverability including superseded and retracted.
		if statusWant != "" && statusWant != "all" {
			st := l.status
			if st == "" {
				st = "active"
			}
			if st != statusWant {
				continue
			}
		}
		if in.ValidOn != "" {
			if l.validFrom != "" {
				if from, err := time.Parse(time.RFC3339, l.validFrom); err == nil && validOnTime.Before(from) {
					continue
				}
			}
			if l.validTo != "" {
				// validTo is exclusive: the claim no longer holds at
				// or after the boundary instant.
				if to, err := time.Parse(time.RFC3339, l.validTo); err == nil && !validOnTime.Before(to) {
					continue
				}
			}
			// Unknown validity is absent, not the ingestion date: an
			// entry without an interval is never excluded by validOn.
		}
		if q != "" && !strings.Contains(strings.ToLower(l.key+" "+l.scope+" "+l.text), q) {
			continue
		}
		matched = append(matched, l)
	}
	out.Matched = len(matched)
	inspector, _ := s.newDerivedInspector(ctx, a, spaceID)
	for i := in.Offset; i < len(matched); i++ {
		l := matched[i]
		if len(l.refs) > 16 || len(l.sourceRefs) > 16 || len(l.supersedes) > 16 || len(l.derivedFrom) > 16 {
			return nil, domain.TooLarge("stored entry exceeds the 16-reference list limit; repair or partition it before recall")
		}
		if len(out.Entries) >= in.Limit {
			out.HasMore = true
			out.NextOffset = i
			break
		}
		entry := MemoryEntry{Key: l.key, Scope: l.scope, Text: l.text,
			Kind: l.kind, ScopeID: l.scopeID, Status: l.status, EvidenceClass: l.evidenceClass,
			RecordedAt: l.recordedAt, UpdatedAt: l.updatedAt, ValidFrom: l.validFrom, ValidTo: l.validTo}
		if l.hasV2() {
			sv := l.schemaVersion
			if sv == 0 {
				sv = MemorySchemaVersion
			}
			entry.SchemaVersion = sv
		}
		for _, sr := range l.sourceRefs {
			entry.SourceRefs = append(entry.SourceRefs, s.recallSourceRef(ctx, a, spaceID, sr))
		}
		entry.Supersedes = append([]MemoryClaimRef(nil), l.supersedes...)
		entry.DerivedFrom = append([]MemoryClaimRef(nil), l.derivedFrom...)
		if len(l.derivedFrom) > 0 {
			entry.DerivedState = s.memoryClaimsState(ctx, a, spaceID, l.derivedFrom)
		}
		for _, target := range l.refs {
			ref := s.recallRef(ctx, a, spaceID, target)
			if inspector != nil && ref.Available && ref.DocumentID != "" {
				state, err := inspector.inspect(ref.DocumentID, 0, nil)
				if err != nil {
					return nil, err
				}
				if state != nil {
					ref.DerivedStatus = state.Status
				}
			}
			entry.Refs = append(entry.Refs, ref)
		}
		if l.detail != "" {
			r := s.recallRef(ctx, a, spaceID, l.detail)
			if inspector != nil && r.Available && r.DocumentID != "" {
				state, err := inspector.inspect(r.DocumentID, 0, nil)
				if err != nil {
					return nil, err
				}
				if state != nil {
					r.DerivedStatus = state.Status
				}
			}
			entry.Detail = &r
		}
		out.Entries = append(out.Entries, entry)
		out.HasMore = i+1 < len(matched)
		if out.HasMore {
			out.NextOffset = i + 1
		} else {
			out.NextOffset = 0
		}
		out.NextCursor = memoryNextCursor(spaceID, generation, in, out)
		encoded, _ := json.Marshal(out)
		if len(encoded) > in.MaxBytes {
			out.Entries = out.Entries[:len(out.Entries)-1]
			if len(out.Entries) == 0 {
				return nil, domain.TooLarge("one memory entry exceeds maxBytes; increase the budget")
			}
			out.HasMore = true
			out.NextOffset = i
			out.Truncated = true
			break
		}
	}
	out.NextCursor = memoryNextCursor(spaceID, generation, in, out)
	latest, err := s.Docs.MaxEventSeq(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if latest != generation {
		return nil, domain.Conflict("vault changed during memory recall; restart retrieval")
	}
	if len(encodeMemoryEnvelope(out)) > in.MaxBytes {
		return nil, domain.TooLarge("memory response exceeds maxBytes")
	}

	return out, nil
}

func encodeMemoryEnvelope(out *MemoryRecallResult) []byte {
	b, _ := json.Marshal(out)
	return b
}
