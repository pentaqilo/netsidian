package service

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/markdown"
)

// Compact indexed agent memory: a short index document in the ordinary
// agent-memory folder holds one line per entry with a stable key, short
// text, an optional scope and references to existing vault resources.
// Long details live in a linked detail document and are never duplicated
// in the index. The index is a regular Markdown note with normal vault
// permissions, revisions and history; these helpers only parse and merge
// it, every mutation still goes through CreateNote/SaveContent guards.

const MemoryIndexPath = "agent-memory/index.md"

const memoryIndexHeader = "---\ntags: [agent-memory]\n---\n\n# Agent memory index\n\n" +
	"Entries are JSON bullets beginning with `- memory: `. Keep keys stable; other text is preserved.\n"

var memoryKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`)

func MemoryDetailPath(key string) string { return "agent-memory/details/" + key + ".md" }

// MemoryRef is a resolved reference to an existing vault resource.
type MemoryRef struct {
	Target     string `json:"target"`
	Kind       string `json:"kind"` // document|folder|board|map
	DocumentID string `json:"documentId,omitempty"`
	Path       string `json:"path,omitempty"`
	Revision   string `json:"revision,omitempty"`
	Available  bool   `json:"available"`
}

// MemoryEntry is one decoded index line.
type MemoryEntry struct {
	Key    string      `json:"key"`
	Scope  string      `json:"scope,omitempty"`
	Text   string      `json:"text"`
	Refs   []MemoryRef `json:"refs,omitempty"`
	Detail *MemoryRef  `json:"detail,omitempty"`
}

type memoryLine struct {
	key, scope, text string
	refs             []string
	detail           string
}

// parseMemoryIndex extracts entry lines; every other line is data that
// writers must preserve. Malformed entry lines are treated as ordinary
// text so an edited index never loses bytes silently.
func parseMemoryIndex(markdown string) []*memoryLine {
	var out []*memoryLine
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- memory: ") {
			var item struct {
				Key    string   `json:"key"`
				Scope  string   `json:"scope,omitempty"`
				Text   string   `json:"text"`
				Refs   []string `json:"refs,omitempty"`
				Detail string   `json:"detail,omitempty"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(trimmed, "- memory: ")), &item) == nil && memoryKeyRe.MatchString(item.Key) && item.Text != "" {
				for i, r := range item.Refs {
					item.Refs[i] = strings.TrimSuffix(strings.TrimPrefix(r, "[["), "]]")
				}
				item.Detail = strings.TrimSuffix(strings.TrimPrefix(item.Detail, "[["), "]]")
				out = append(out, &memoryLine{key: item.Key, scope: item.Scope, text: item.Text, refs: item.Refs, detail: item.Detail})
			}
			continue
		}
	}
	return out
}

func (l *memoryLine) render() string {
	item := struct {
		Key    string   `json:"key"`
		Scope  string   `json:"scope,omitempty"`
		Text   string   `json:"text"`
		Refs   []string `json:"refs,omitempty"`
		Detail string   `json:"detail,omitempty"`
	}{l.key, l.scope, l.text, l.refs, l.detail}
	item.Refs = append([]string(nil), item.Refs...)
	for i, r := range item.Refs {
		item.Refs[i] = "[[" + r + "]]"
	}
	if item.Detail != "" {
		item.Detail = "[[" + item.Detail + "]]"
	}
	data, _ := json.Marshal(item)
	return "- memory: " + string(data)
}

// mergeMemoryIndex returns the whole document with one entry replaced or
// appended; unrelated entries and any other content stay byte-identical.
func mergeMemoryIndex(markdown string, l *memoryLine) (string, bool) {
	newLine := l.render()
	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- memory: ") {
			continue
		}
		if parsed := parseMemoryIndex(line); len(parsed) == 1 && parsed[0].key == l.key {
			if line == newLine {
				return markdown, false
			}
			lines[i] = newLine
			return strings.Join(lines, "\n"), true
		}
	}
	if !strings.HasSuffix(markdown, "\n") && markdown != "" {
		markdown += "\n"
	}
	return markdown + newLine + "\n", true
}

// MemoryRememberInput is one governed index mutation.
type MemoryRememberInput struct {
	Folder           string   `json:"folder,omitempty"`
	ExpectedRevision string   `json:"expectedRevision,omitempty"`
	Key              string   `json:"key"`
	Text             string   `json:"text"`
	Scope            string   `json:"scope,omitempty"`
	Refs             []string `json:"refs,omitempty"`
	Detail           string   `json:"detail,omitempty"`
	DetailPath       string   `json:"detailPath,omitempty"`
	DetailRevision   string   `json:"detailRevision,omitempty"`
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
	if utf8.RuneCountInString(in.Detail) > 20000 {
		return nil, domain.Invalid("detail exceeds 20000 characters")
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
	if in.ExpectedRevision != "" && (indexNote == nil || indexNote.Revision != in.ExpectedRevision) {
		return nil, domain.Conflict("memory index changed; recall and merge before retrying")
	}
	if indexNote != nil {
		if len(indexNote.Markdown) > 1<<20 {
			return nil, domain.TooLarge("memory index exceeds 1 MiB; split memory into scoped folders")
		}
		found := false
		for _, entry := range parseMemoryIndex(indexNote.Markdown) {
			if entry.key != in.Key {
				continue
			}
			if found {
				return nil, domain.Conflict("duplicate memory keys in index; resolve them before updating")
			}
			found = true
			line.detail = entry.detail
			if in.Refs == nil {
				line.refs = append([]string(nil), entry.refs...)
			}
			if in.Scope == "" {
				line.scope = entry.scope
			}
		}
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
				saved, err := s.SaveContent(ctx, a, spaceID, existing.ID, note.Revision, in.Detail, memoryStepKey(idemKey, "detail"))
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

	if index == nil || index.TombstoneAt != nil {
		created, err := s.CreateNote(ctx, a, spaceID, indexPath, memoryIndexHeader+line.render()+"\n", memoryStepKey(idemKey, "index"), false)
		if err != nil {
			return nil, err
		}
		receipt.Status, receipt.IndexRevision = "created", created.CurrentRevision
	} else {
		note := indexNote
		merged, changed := mergeMemoryIndex(note.Markdown, line)
		if !changed {
			receipt.Status, receipt.IndexRevision = "unchanged", note.Revision
		} else {
			saved, err := s.SaveContent(ctx, a, spaceID, index.ID, note.Revision, merged, memoryStepKey(idemKey, "index"))
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
type MemoryRecallInput struct {
	Folder   string `json:"folder,omitempty"`
	Revision string `json:"revision,omitempty"`
	Query    string `json:"query,omitempty"`
	Scope    string `json:"scope,omitempty"`
	Key      string `json:"key,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	MaxBytes int    `json:"maxBytes,omitempty"`
}

// MemoryRecallResult is bounded: entries past the limit or byte budget
// are reported through hasMore/nextOffset, never silently dropped.
type MemoryRecallResult struct {
	IndexPath     string        `json:"indexPath"`
	IndexRevision string        `json:"indexRevision,omitempty"`
	Entries       []MemoryEntry `json:"entries"`
	Matched       int           `json:"matched"`
	Offset        int           `json:"offset"`
	HasMore       bool          `json:"hasMore"`
	NextOffset    int           `json:"nextOffset,omitempty"`
	Truncated     bool          `json:"truncated,omitempty"`
	Note          string        `json:"note,omitempty"`
}

func (s *Services) recallRef(ctx context.Context, a *Actor, spaceID, target string) MemoryRef {
	ref, err := s.resolveMemoryRef(ctx, a, spaceID, target)
	if err != nil || ref == nil {
		return MemoryRef{Target: target, Kind: "unknown"}
	}
	return *ref
}

// RecallMemory returns filtered index entries and their resolved links
// only; linked document bodies are never included.
func (s *Services) RecallMemory(ctx context.Context, a *Actor, spaceID string, in *MemoryRecallInput) (*MemoryRecallResult, error) {
	if _, err := s.Require(ctx, a, spaceID, domain.ActionRead); err != nil {
		return nil, err
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
	out := &MemoryRecallResult{IndexPath: indexPath, Entries: []MemoryEntry{}, Offset: in.Offset}
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
	if in.Revision != "" && in.Revision != note.Revision {
		return nil, domain.Conflict("memory index changed; restart recall")
	}
	if len(note.Markdown) > 1<<20 {
		return nil, domain.TooLarge("memory index exceeds 1 MiB; split memory into scoped folders")
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	scopeWant := strings.ToLower(strings.TrimSpace(in.Scope))
	var matched []*memoryLine
	for _, l := range parseMemoryIndex(note.Markdown) {
		if in.Key != "" && l.key != in.Key {
			continue
		}
		if scopeWant != "" && strings.ToLower(l.scope) != scopeWant {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(l.key+" "+l.scope+" "+l.text), q) {
			continue
		}
		matched = append(matched, l)
	}
	out.Matched = len(matched)
	for i := in.Offset; i < len(matched); i++ {
		l := matched[i]
		if len(out.Entries) >= in.Limit {
			out.HasMore = true
			out.NextOffset = i
			break
		}
		entry := MemoryEntry{Key: l.key, Scope: l.scope, Text: l.text}
		for _, target := range l.refs {
			entry.Refs = append(entry.Refs, s.recallRef(ctx, a, spaceID, target))
		}
		if l.detail != "" {
			r := s.recallRef(ctx, a, spaceID, l.detail)
			entry.Detail = &r
		}
		out.Entries = append(out.Entries, entry)
		out.HasMore = i+1 < len(matched)
		if out.HasMore {
			out.NextOffset = i + 1
		} else {
			out.NextOffset = 0
		}
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
	if len(encodeMemoryEnvelope(out)) > in.MaxBytes {
		return nil, domain.TooLarge("memory response exceeds maxBytes")
	}

	return out, nil
}

func encodeMemoryEnvelope(out *MemoryRecallResult) []byte {
	b, _ := json.Marshal(out)
	return b
}
