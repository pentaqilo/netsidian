package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

func copyMemoryInput(in *MemoryRememberInput) *MemoryRememberInput {
	copy := *in
	copy.Refs = slices.Clone(in.Refs)
	copy.SourceRefs = slices.Clone(in.SourceRefs)
	copy.Supersedes = slices.Clone(in.Supersedes)
	copy.DerivedFrom = slices.Clone(in.DerivedFrom)
	copy.ClearFields = slices.Clone(in.ClearFields)
	return &copy
}

// Unknown nested metadata survives changes to understood fields, including an
// appended supersedes pin. Unrelated index lines never pass through this merge.
func preserveStoredJSON(raw, old, next json.RawMessage) json.RawMessage {
	if bytes.Equal(old, next) {
		return raw
	}
	var rmap, omap, nmap map[string]json.RawMessage
	if json.Unmarshal(raw, &rmap) == nil && json.Unmarshal(old, &omap) == nil && json.Unmarshal(next, &nmap) == nil && rmap != nil && omap != nil && nmap != nil {
		for key, value := range rmap {
			if _, known := omap[key]; !known {
				if _, replaced := nmap[key]; !replaced {
					nmap[key] = value
				}
			}
		}
		for key, value := range nmap {
			if previous, ok := omap[key]; ok {
				if original, ok := rmap[key]; ok {
					nmap[key] = preserveStoredJSON(original, previous, value)
				}
			}
		}
		result, _ := json.Marshal(nmap)
		return result
	}
	var rarr, oarr, narr []json.RawMessage
	if json.Unmarshal(raw, &rarr) == nil && json.Unmarshal(old, &oarr) == nil && json.Unmarshal(next, &narr) == nil && rarr != nil && oarr != nil && narr != nil {
		for index := range narr {
			if index < len(oarr) && index < len(rarr) {
				var before, after map[string]json.RawMessage
				_ = json.Unmarshal(oarr[index], &before)
				_ = json.Unmarshal(narr[index], &after)
				same := true
				for _, identity := range []string{"target", "key", "indexRevision", "documentId", "revision"} {
					if !bytes.Equal(before[identity], after[identity]) {
						same = false
					}
				}
				if same {
					narr[index] = preserveStoredJSON(rarr[index], oarr[index], narr[index])
				}
			}
		}
		result, _ := json.Marshal(narr)
		return result
	}
	return next
}
func memoryIndexGuard(markdown, target string) error {
	keys := map[string]bool{}
	for _, line := range strings.Split(markdown, "\n") {
		text := strings.TrimSpace(line)
		if !strings.HasPrefix(text, "- memory: ") {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(strings.TrimPrefix(text, "- memory: ")), &fields) != nil {
			continue
		}
		var key string
		if json.Unmarshal(fields["key"], &key) != nil || !memoryKeyRe.MatchString(key) {
			continue
		}
		if keys[key] {
			return domain.Conflict("duplicate memory keys in index; resolve ambiguity before mutation")
		}
		keys[key] = true
		if key == target {
			if _, ok := parseMemoryLine(line); !ok {
				return domain.Unsupported("existing entry metadata is malformed or unsupported; repair it explicitly before updating")
			}
		}
	}
	return nil
}
func memoryIndexDiagnostics(markdown string) []string {
	diagnostics := []string{}
	seen := map[string]bool{}
	for number, line := range strings.Split(markdown, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "- memory: ") {
			continue
		}
		parsed, ok := parseMemoryLine(line)
		if !ok {
			diagnostics = append(diagnostics, fmt.Sprintf("Malformed memory entry at line %d; preserved as data", number+1))
		} else if seen[parsed.key] {
			diagnostics = append(diagnostics, fmt.Sprintf("Duplicate memory key at line %d; mutations are ambiguous", number+1))
		} else {
			seen[parsed.key] = true
		}
		if len(diagnostics) >= 16 {
			break
		}
	}
	return diagnostics
}
func clearMemoryFields(line *memoryLine, fields []string) error {
	if len(fields) > 8 {
		return domain.Invalid("at most 8 clearFields are allowed")
	}
	for _, field := range fields {
		switch field {
		case "kind":
			line.kind = ""
		case "scope":
			line.scope = ""
		case "scopeId":
			line.scopeID = ""
		case "evidenceClass":
			line.evidenceClass = ""
		case "validFrom":
			line.validFrom = ""
		case "validTo":
			line.validTo = ""
		case "sourceRefs":
			line.sourceRefs = nil
		case "derivedFrom":
			line.derivedFrom = nil
		default:
			return domain.Invalid("clearFields may contain kind, scope, scopeId, evidenceClass, validFrom, validTo, sourceRefs or derivedFrom")
		}
	}
	return nil
}
func validateMergedMemory(line *memoryLine) error {
	if line.kind != "" && !memoryKinds[line.kind] || line.status != "" && !memoryStatuses[line.status] || line.evidenceClass != "" && !memoryEvidenceClasses[line.evidenceClass] {
		return domain.Invalid("stored or merged memory metadata has unsupported enum values")
	}
	if len(line.sourceRefs) > 16 || len(line.supersedes) > 16 || len(line.derivedFrom) > 16 {
		return domain.TooLarge("at most 16 references per evidence/claim list are supported")
	}
	if (line.kind == "inference" || line.evidenceClass == "inferred") && len(line.sourceRefs) == 0 && len(line.derivedFrom) == 0 {
		return domain.Invalid("inference requires pinned sourceRefs or derivedFrom evidence")
	}
	for name, value := range map[string]string{"validFrom": line.validFrom, "validTo": line.validTo} {
		if value != "" {
			if _, err := parseMemoryTime(name, value); err != nil {
				return err
			}
		}
	}
	if line.validFrom != "" && line.validTo != "" {
		from, e1 := time.Parse(time.RFC3339Nano, line.validFrom)
		to, e2 := time.Parse(time.RFC3339Nano, line.validTo)
		if e1 != nil || e2 != nil || from.After(to) {
			return domain.Invalid("merged factual interval is invalid")
		}
	}
	return nil
}
func (s *Services) resolveMemoryClaim(ctx context.Context, a *Actor, space string, index *domain.Document, ref *MemoryClaimRef) error {
	for name, value := range map[string]string{"claim.vaultId": ref.VaultID, "claim.indexDocumentId": ref.IndexDocumentID, "claim.indexRevision": ref.IndexRevision} {
		if err := validateMemoryField(name, value, 128); err != nil {
			return err
		}
	}
	if ref.VaultID != "" && ref.VaultID != space {
		return domain.Forbidden("cross-vault claim evidence is not supported")
	}
	if ref.IndexDocumentID == "" && index != nil {
		ref.IndexDocumentID = index.ID
	}
	ref.VaultID = space
	if !memoryKeyRe.MatchString(ref.Key) || ref.IndexDocumentID == "" {
		return domain.Invalid("claim reference requires an existing index document and key")
	}
	document, err := s.Docs.Document(ctx, ref.IndexDocumentID)
	if err != nil {
		return err
	}
	if document == nil || document.SpaceID != space || document.TombstoneAt != nil {
		return domain.Invalid("claim index is unavailable in this vault")
	}
	note, err := s.ReadNote(ctx, a, space, document.ID)
	if err != nil {
		return err
	}
	body := note.Markdown
	if ref.IndexRevision == "" {
		ref.IndexRevision = note.Revision
	}
	if ref.IndexRevision != note.Revision {
		body, err = s.RevisionContent(ctx, a, space, document.ID, ref.IndexRevision)
		if err != nil {
			return err
		}
	}
	if err := memoryIndexGuard(body, ref.Key); err != nil {
		return err
	}
	for _, line := range parseMemoryIndex(body) {
		if line.key == ref.Key {
			ref.Fingerprint = claimFingerprint(line)
			ref.ExpectedStatus = memoryStatus(line.status)
			return nil
		}
	}
	return domain.Invalid("claim key is not present at the pinned index revision")
}

func (s *Services) memoryClaimsState(ctx context.Context, a *Actor, space string, refs []MemoryClaimRef) string {
	return s.boundedMemoryClaimsState(ctx, a, space, refs)
}
