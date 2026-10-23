package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

// Execution of consolidation plans. The plan, frozen target snapshots
// and deterministic step identifiers are journaled before any write.
// Each step writes through the ordinary guarded document services under
// its own idempotency key, so a crash between a committed write and a
// journal checkpoint is reconciled by proving the actual child
// operation and its exact bytes, never by trusting recorded status.
// A failed or blocked step stops the run and yields an honest partial
// receipt; applied steps and any later user edits are never rolled
// back. Resume re-verifies applied steps against the current vault and
// reports a conflict instead of overwriting edits.

const consolidateJournalVersion = 1

func validateConsolidatePlan(in *MemoryConsolidateInput) error {
	if in.OperationID == "" || len(in.OperationID) > 256 || validateMemoryField("operationId", in.OperationID, 256) != nil || strings.ContainsAny(in.OperationID, " \t\n") {
		return domain.Invalid("operationId is required, must not contain whitespace and is at most 256 characters")
	}
	if len(in.Sources) > consolidateMaxSources {
		return domain.Invalid("at most 16 sources are allowed")
	}
	if len(in.Targets) > consolidateMaxTargets {
		return domain.Invalid("at most 1 target document is allowed per consolidation")
	}
	if len(in.Entries) > consolidateMaxEntries {
		return domain.Invalid("at most 32 entry changes are allowed")
	}
	for _, src := range in.Sources {
		if (src.Key == "") == (src.DocumentID == "") {
			return domain.Invalid("each source needs exactly one key or documentId")
		}
		if src.Key != "" && !memoryKeyRe.MatchString(src.Key) {
			return domain.Invalid("source key must match ^[a-z0-9][a-z0-9._-]{0,95}$")
		}
		if src.DocumentID != "" && src.ExpectedRevision == "" {
			return domain.Invalid("document sources must pin an expectedRevision")
		}
	}
	for _, t := range in.Targets {
		if (t.DocumentID == "") == (t.Path == "") {
			return domain.Invalid("each target needs exactly one documentId or path")
		}
		if t.Markdown == nil {
			return domain.Invalid("target markdown is required; pass an empty string to empty the managed section")
		}
		if utf8Len(*t.Markdown) > consolidateMaxDetail {
			return domain.TooLarge("target markdown exceeds 20000 characters")
		}
		if strings.Contains(*t.Markdown, memoryManagedStart) || strings.Contains(*t.Markdown, memoryManagedEnd) {
			return domain.Invalid("target markdown must not contain ownership delimiters")
		}
	}
	seen := map[string]bool{}
	for _, e := range in.Entries {
		if !memoryKeyRe.MatchString(e.Key) {
			return domain.Invalid("entry key must match ^[a-z0-9][a-z0-9._-]{0,95}$")
		}
		if seen[e.Key] {
			return domain.Invalid("duplicate entry key in plan: " + e.Key)
		}
		seen[e.Key] = true
		if e.Status != "" && !memoryStatuses[e.Status] {
			return domain.Invalid("entry status must be active, superseded, disputed or retracted")
		}
		if e.Text != "" && utf8Len(e.Text) > 500 {
			return domain.Invalid("entry text is at most 500 characters")
		}
		if len(e.Supersedes) > 16 {
			return domain.Invalid("at most 16 supersedes keys are allowed per entry")
		}
		for _, k := range e.Supersedes {
			if !memoryKeyRe.MatchString(k) {
				return domain.Invalid("supersedes key must match ^[a-z0-9][a-z0-9._-]{0,95}$")
			}
		}
		if e.Status == "" && e.Text == "" && e.Supersedes == nil {
			return domain.Invalid("entry change requires a status, text or supersedes change")
		}
	}
	return nil
}

// consolidateTargetState resolves a plan target to its current document
// (nil when it does not exist yet) and readable body.
func (s *Services) consolidateTargetState(ctx context.Context, a *Actor, spaceID string, t *MemoryConsolidateTarget) (*domain.Document, string, error) {
	var doc *domain.Document
	var err error
	if t.DocumentID != "" {
		doc, err = s.Docs.Document(ctx, t.DocumentID)
		if err != nil {
			return nil, "", err
		}
		if doc != nil && (doc.SpaceID != spaceID || doc.TombstoneAt != nil) {
			return nil, "", domain.NotFound("target document is unavailable")
		}
	} else {
		canon, key, perr := domain.NormalizePath(t.Path)
		if perr != nil {
			return nil, "", perr
		}
		if !strings.HasSuffix(strings.ToLower(canon), ".md") {
			return nil, "", domain.Invalid("target path must end in .md")
		}
		doc, err = s.Docs.DocumentByNormKey(ctx, spaceID, key)
		if err != nil {
			return nil, "", err
		}
		if doc != nil && doc.TombstoneAt != nil {
			return nil, "", domain.Conflict("target document is in trash; restore it explicitly first")
		}
	}
	if doc == nil {
		return nil, "", nil
	}
	note, err := s.ReadNote(ctx, a, spaceID, doc.ID)
	if err != nil {
		return nil, "", err
	}
	return doc, note.Markdown, nil
}

// applyConsolidateEntries merges the plan's entry changes into an
// index body, pinning supersedes claims at the revision being written.
func (s *Services) applyConsolidateEntries(ctx context.Context, a *Actor, spaceID string, index *domain.Document, body string, entries []MemoryConsolidateEntry) (string, error) {
	for _, e := range entries {
		var existing *memoryLine
		for _, l := range parseMemoryIndex(body) {
			if l.key != e.Key {
				continue
			}
			if existing != nil {
				return "", domain.Conflict("duplicate memory keys in index; resolve them before consolidating: " + e.Key)
			}
			existing = l
		}
		if existing == nil {
			return "", domain.NotFound("memory key not found in index: " + e.Key)
		}
		if existing.schemaVersion > MemorySchemaVersion {
			return "", domain.Unsupported("memory entry uses a newer schema version; this server refuses to rewrite it: " + e.Key)
		}
		if err := memoryIndexGuard(body, e.Key); err != nil {
			return "", err
		}
		line := *existing
		line.refs = append([]string(nil), existing.refs...)
		line.sourceRefs = append([]MemorySourceRef(nil), existing.sourceRefs...)
		line.supersedes = append([]MemoryClaimRef(nil), existing.supersedes...)
		line.derivedFrom = append([]MemoryClaimRef(nil), existing.derivedFrom...)
		line.extra = map[string]json.RawMessage{}
		for k, v := range existing.extra {
			line.extra[k] = v
		}
		if e.Status != "" {
			line.status = e.Status
		}
		if e.Text != "" && e.Text != existing.text {
			previous := MemoryClaimRef{VaultID: spaceID, IndexDocumentID: index.ID, Key: e.Key, IndexRevision: index.CurrentRevision}
			if err := s.resolveMemoryClaim(ctx, a, spaceID, index, &previous); err != nil {
				return "", err
			}
			line.supersedes = append(line.supersedes, previous)
			line.text = e.Text
		}
		for _, k := range e.Supersedes {
			claim := MemoryClaimRef{VaultID: spaceID, Key: k}
			if err := s.resolveMemoryClaim(ctx, a, spaceID, index, &claim); err != nil {
				return "", err
			}
			line.supersedes = append(line.supersedes, claim)
		}
		if err := validateMergedMemory(&line); err != nil {
			return "", err
		}
		if line.render() == existing.render() {
			continue
		}
		if len(line.supersedes) > 16 {
			return "", domain.TooLarge("claim lineage exceeds 16 references; use historical links rather than expanding one entry")
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if line.recordedAt == "" || line.text != existing.text {
			line.recordedAt = now
		}
		line.updatedAt = now
		body, _ = mergeMemoryIndex(body, &line)
	}
	return body, nil
}

// ConsolidateMemory freezes a bounded agent-authored plan before any knowledge
// write. A journal is recovery state; Markdown remains canonical knowledge.
func (s *Services) ConsolidateMemory(ctx context.Context, a *Actor, space string, in *MemoryConsolidateInput) (result *MemoryConsolidationReceipt, returned error) {
	if _, err := s.Require(ctx, a, space, domain.ActionRead); err != nil {
		return nil, err
	}
	if _, err := s.Require(ctx, a, space, domain.ActionEdit); err != nil {
		return nil, err
	}
	if in == nil {
		return nil, domain.Invalid("consolidation plan required")
	}
	encoded, _ := json.Marshal(in)
	if len(encoded) > 96<<10 {
		return nil, domain.TooLarge("consolidation plan exceeds 96 KiB; split it into smaller plans")
	}
	var plan MemoryConsolidateInput
	if err := json.Unmarshal(encoded, &plan); err != nil {
		return nil, domain.Invalid("invalid consolidation plan")
	}
	if err := validateConsolidatePlan(&plan); err != nil {
		return nil, err
	}
	if len(plan.Entries) > 0 && plan.ExpectedRevision == "" {
		return nil, domain.PreconditionRequired("expectedRevision is required for index entry changes")
	}
	if len(plan.Targets) > 0 && len(plan.Sources) == 0 {
		return nil, domain.Invalid("a derived target requires pinned document or claim sources")
	}
	folder, err := memoryFolder(plan.Folder)
	if err != nil {
		return nil, err
	}
	indexPath := folder + "/index.md"
	defer func() {
		if plan.DryRun && returned != nil {
			var de *domain.Error
			if errors.As(returned, &de) && de != nil && (de.Status == 409 || de.Status == 412 || de.Status == 428 || de.Status == 404) {
				result = &MemoryConsolidationReceipt{OperationID: plan.OperationID, Outcome: "conflicts", IndexPath: indexPath, Steps: []ConsolidationStep{}, Warnings: []string{de.Message}, Verified: false}
				returned = nil
			}
		}
	}()

	opLock := s.memoryMu("consolidate:" + space + ":" + a.UserID)
	opLock.Lock()
	defer opLock.Unlock()
	lock := s.memoryMu(space + ":" + folder)
	lock.Lock()
	defer lock.Unlock()
	hashPlan := plan
	hashPlan.DryRun = false
	data, _ := json.Marshal(hashPlan)
	hash := shaHex(data)
	prior, err := s.Docs.FindOperationByIdempotency(ctx, space, a.UserID, consolidateOpKey(plan.OperationID))
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.Kind != domain.OperationKind("memory_consolidate") || prior.RequestHash != hash {
			return nil, domain.Conflict("operationId belongs to a different plan")
		}
		var journal consolidateJournal
		if json.Unmarshal([]byte(prior.Error), &journal) != nil || journal.Version != consolidateJournalVersion || journal.Plan == nil {
			return nil, domain.Unavailable("completion unknown: recovery journal is unreadable; retain original operationId")
		}
		if plan.DryRun {
			return s.consolidationReceipt(ctx, space, indexPath, prior.ID, &journal, "dry-run", false), nil
		}
		if prior.Phase == domain.OpPhaseCommitted && journal.Receipt != nil {
			return journal.Receipt, nil
		}
		if prior.Phase != domain.OpPhasePending {
			return nil, domain.Conflict("operation is blocked; inspect its recovery status")
		}
		return s.runConsolidation(ctx, a, space, indexPath, prior.ID, &journal)
	}
	index, err := s.Docs.DocumentByNormKey(ctx, space, mustNormKey(indexPath))
	if err != nil {
		return nil, err
	}
	if index != nil && index.TombstoneAt != nil {
		return nil, domain.Conflict("memory index is in trash")
	}
	journal := &consolidateJournal{Version: consolidateJournalVersion, Plan: &hashPlan, Steps: []ConsolidationStep{}, Evidence: []MemorySourceRef{}, Claims: []MemoryClaimRef{}}
	if index != nil {
		note, err := s.ReadNote(ctx, a, space, index.ID)
		if err != nil {
			return nil, err
		}
		if len(note.Markdown) > 1<<20 {
			return nil, domain.TooLarge("memory index exceeds 1 MiB; partition scoped folders")
		}
		if err := memoryIndexGuard(note.Markdown, ""); err != nil {
			return nil, err
		}
		journal.IndexID, journal.IndexRevision, journal.IndexMarkdown = index.ID, note.Revision, note.Markdown
	}
	if plan.ExpectedRevision != "" && plan.ExpectedRevision != journal.IndexRevision {
		return nil, domain.Conflict("memory index changed; rebuild the plan against its current revision")
	}
	if len(plan.Entries) > 0 && index == nil {
		return nil, domain.Conflict("index entries do not exist")
	}
	for _, source := range plan.Sources {
		if source.Key != "" {
			if index == nil {
				return nil, domain.Invalid("claim source index unavailable")
			}
			pin := source.ExpectedRevision
			if pin == "" {
				pin = plan.ExpectedRevision
			}
			if pin == "" {
				return nil, domain.PreconditionRequired("claim sources require an expected index revision")
			}
			if pin != journal.IndexRevision {
				return nil, domain.Conflict("claim source index changed")
			}
			claim := MemoryClaimRef{VaultID: space, IndexDocumentID: index.ID, Key: source.Key, IndexRevision: pin}
			if err := s.resolveMemoryClaim(ctx, a, space, index, &claim); err != nil {
				return nil, err
			}
			journal.Claims = append(journal.Claims, claim)
		} else {
			ref := MemorySourceRef{Target: source.DocumentID, Kind: "document", Revision: source.ExpectedRevision}
			if err := s.resolveMemorySourceRef(ctx, a, space, &ref); err != nil {
				return nil, err
			}
			doc, err := s.Docs.Document(ctx, ref.DocumentID)
			if err != nil {
				return nil, err
			}
			if doc == nil || doc.CurrentRevision != source.ExpectedRevision {
				return nil, domain.Conflict("source revision changed; review current evidence before consolidating")
			}
			journal.Evidence = append(journal.Evidence, ref)
		}
	}
	originalIndex := journal.IndexMarkdown
	if len(plan.Entries) > 0 {
		journal.IndexMarkdown, err = s.applyConsolidateEntries(ctx, a, space, index, originalIndex, plan.Entries)
		if err != nil {
			return nil, err
		}
		if len(journal.IndexMarkdown) > 1<<20 {
			return nil, domain.TooLarge("consolidated index exceeds 1 MiB; partition scoped folders")
		}
	}
	// A target cannot claim to be derived from a source whose meaning is changed
	// by the same plan. Corrections are a separate guarded operation.
	if len(plan.Targets) > 0 {
		for _, claim := range journal.Claims {
			for _, change := range plan.Entries {
				if change.Key == claim.Key && (change.Text != "" || len(change.Supersedes) > 0) {
					return nil, domain.Invalid("revise a source claim before deriving a target from its new meaning")
				}
			}
		}
	}
	targetClaims := append([]MemoryClaimRef(nil), journal.Claims...)
	for i := range targetClaims {
		for _, change := range plan.Entries {
			if change.Key == targetClaims[i].Key && change.Status != "" {
				targetClaims[i].ExpectedStatus = change.Status
			}
		}
	}
	opID := domain.NewID("op")
	for _, target := range plan.Targets {
		doc, body, err := s.consolidateTargetState(ctx, a, space, &target)
		if err != nil {
			return nil, err
		}
		if target.DocumentID != "" && doc == nil {
			return nil, domain.NotFound("target document unavailable")
		}
		path := target.Path
		if doc != nil {
			path = doc.Path
			if target.ExpectedRevision == "" {
				return nil, domain.PreconditionRequired("existing target requires expectedRevision")
			}
			if target.ExpectedRevision != doc.CurrentRevision {
				return nil, domain.Conflict("target revision changed")
			}
		} else {
			canon, _, pathErr := domain.NormalizePath(path)
			if pathErr != nil {
				return nil, pathErr
			}
			path = canon
			if target.ExpectedRevision != "" {
				return nil, domain.Conflict("target expected revision has no current document")
			}
		}
		if path == indexPath || doc != nil && doc.ID == journal.IndexID {
			return nil, domain.Invalid("derived target cannot replace its memory index")
		}
		targetID := ""
		if doc != nil {
			targetID = doc.ID
		}
		if err := s.checkConsolidationDependencies(ctx, a, space, journal, targetID, false); err != nil {
			return nil, err
		}
		final, err := managedMemoryTarget(body, *target.Markdown, opID, journal.Evidence, targetClaims, target.AdoptManagedSection || doc == nil)
		if err != nil {
			return nil, err
		}
		if int64(len(final)) > s.Cfg.MaxNoteBytes {
			return nil, domain.TooLarge("prepared target exceeds the instance document limit")
		}
		step := ConsolidationStep{ID: consolidateStepID(plan.OperationID, "target", path), Kind: "target", Path: path, DocumentID: targetID, Status: "planned"}
		journal.Steps = append(journal.Steps, step)
		journal.TargetMarkdown = append(journal.TargetMarkdown, final)
	}
	if len(plan.Entries) > 0 && journal.IndexMarkdown != originalIndex {
		journal.Steps = append(journal.Steps, ConsolidationStep{ID: consolidateStepID(plan.OperationID, "index", indexPath), Kind: "index", DocumentID: index.ID, Path: indexPath, Status: "planned"})
	}
	if err := s.checkConsolidationDependencies(ctx, a, space, journal, "", false); err != nil {
		return nil, err
	}
	if plan.DryRun {
		return s.consolidationReceipt(ctx, space, indexPath, opID, journal, "dry-run", false), nil
	}
	bytes, _ := json.Marshal(journal)
	now := time.Now()
	if err := s.Docs.InsertOperation(ctx, &domain.Operation{ID: opID, SpaceID: space, Principal: a.UserID, Kind: domain.OperationKind("memory_consolidate"), Phase: domain.OpPhasePending, RequestHash: hash, IdempotencyKey: consolidateOpKey(plan.OperationID), Error: string(bytes), CreatedAt: now, UpdatedAt: now}); err != nil {
		return nil, domain.Unavailable("recovery journal unavailable; no knowledge writes attempted")
	}
	return s.runConsolidation(ctx, a, space, indexPath, opID, journal)
}

func (s *Services) checkConsolidationDependencies(ctx context.Context, a *Actor, space string, j *consolidateJournal, target string, postIndex bool) error {
	ctx = memoryClaimContext(ctx, s, a, space)
	checker := ctx.Value(memoryClaimContextKey{}).(*memoryClaimInspector)
	inspector, err := s.newDerivedInspector(ctx, a, space)
	if err != nil {
		return err
	}
	for _, ref := range j.Evidence {
		d, err := s.Docs.Document(ctx, ref.DocumentID)
		if err != nil {
			return err
		}
		if d == nil || d.SpaceID != space || d.TombstoneAt != nil || d.CurrentRevision != ref.Revision {
			return domain.Conflict("evidence changed or became unavailable; retain this operation for recovery")
		}
		if d.ID == target {
			return domain.Invalid("derived target cannot depend on itself")
		}
		status, err := inspector.inspect(d.ID, 0, nil)
		if err != nil {
			return err
		}
		if status != nil && status.Status != "current" {
			return domain.Conflict("derived source is not current; review its evidence first")
		}
	}
	claims := append([]MemoryClaimRef(nil), j.Claims...)
	if postIndex {
		for i := range claims {
			for _, change := range j.Plan.Entries {
				if change.Key == claims[i].Key {
					if len(j.Plan.Targets) == 0 {
						claims[i].Key = ""
						break
					}
					if change.Status != "" {
						claims[i].ExpectedStatus = change.Status
					}
				}
			}
		}
	}
	for _, claim := range claims {
		if claim.Key == "" {
			continue
		}
		state := checker.claim(claim, 0)
		if state != "current" {
			return domain.Conflict("claim evidence is " + state + "; review it before continuing consolidation")
		}
	}
	if target != "" {
		if inspector.seen[target] {
			return domain.Invalid("derived dependency would form a cycle")
		}
		if _, seen := checker.documents[target]; seen {
			return domain.Invalid("claim dependency would form a cycle with its target")
		}
	}
	return nil
}
func (s *Services) consolidationReceipt(ctx context.Context, space, indexPath, opID string, j *consolidateJournal, outcome string, verified bool) *MemoryConsolidationReceipt {
	out := &MemoryConsolidationReceipt{OperationID: j.Plan.OperationID, Outcome: outcome, IndexPath: indexPath, IndexRevision: j.IndexRevision, Steps: append([]ConsolidationStep{}, j.Steps...), Warnings: j.Warnings, Verified: verified, Evidence: j.Evidence, Claims: j.Claims}
	for _, step := range j.Steps {
		if step.Kind == "index" && step.Status == "applied" {
			out.IndexRevision = step.Revision
		}
	}
	if outcome == "partial" {
		out.RecoveryRef = memoryRecoveryRef(space, j.Plan.OperationID)
		out.RecoveryNote = "Inspect recovery status, preserve applied steps and manual edits, then retry the original operationId and identical plan. A changed plan needs a distinct identity only after recovery is understood."
	}
	return out
}
func consolidationWanted(j *consolidateJournal, i int) string {
	if j.Steps[i].Kind == "index" {
		return j.IndexMarkdown
	}
	return j.TargetMarkdown[i]
}

// An applied checkpoint is only a hint. Actual child ownership, produced revision
// and exact current bytes are required even when the checkpoint was lost.
func (s *Services) proveConsolidationStep(ctx context.Context, a *Actor, space string, j *consolidateJournal, i int) (bool, error) {
	step := &j.Steps[i]
	want := consolidationWanted(j, i)
	child, err := s.Docs.FindOperationByIdempotency(ctx, space, a.UserID, step.ID)
	if err != nil {
		return false, err
	}
	if child == nil {
		if step.Status == "applied" {
			return false, domain.Conflict("applied checkpoint has no committed child proof")
		}
		return false, nil
	}
	if child.Phase != domain.OpPhaseCommitted || child.SpaceID != space || child.DocumentID == "" || child.NewSHA256 != shaHex([]byte(want)) || step.DocumentID != "" && step.DocumentID != child.DocumentID {
		return false, domain.Conflict("child completion unknown or differs from frozen plan")
	}
	expected := j.IndexRevision
	if step.Kind == "target" {
		expected = j.Plan.Targets[i].ExpectedRevision
	}
	if expected != "" {
		if child.Kind != domain.OpKindContent || child.ExpectedRevision != expected {
			return false, domain.Conflict("child revision guard does not match the frozen plan")
		}
	} else if child.Kind != domain.OpKindCreate {
		return false, domain.Conflict("expected a created target child")
	}
	revisions, err := s.Docs.RevisionsForOperation(ctx, child.ID)
	if err != nil {
		return false, err
	}
	produced := ""
	for _, revision := range revisions {
		if revision.DocumentID == child.DocumentID && revision.SHA256 == child.NewSHA256 {
			produced = revision.ID
		}
	}
	if produced == "" {
		return false, domain.Unavailable("child revision has not been reconciled")
	}
	note, err := s.ReadNote(ctx, a, space, child.DocumentID)
	if err != nil {
		return false, err
	}
	working, workingErr := s.Content.Read(space, note.Path)
	if workingErr != nil || shaHex(working) != child.NewSHA256 {
		return false, domain.Conflict("applied target has unreconciled storage edits; preserve them before retrying")
	}
	if note.Revision != produced || note.Markdown != want {
		return false, domain.Conflict("manual edit followed the applied step; preserve it and reconcile")
	}
	step.DocumentID, step.Revision, step.Status, step.Error = child.DocumentID, produced, "applied", ""
	return true, nil
}
func (s *Services) runConsolidation(ctx context.Context, a *Actor, space, indexPath, opID string, j *consolidateJournal) (*MemoryConsolidationReceipt, error) {
	partial := func(i int, err error, code string) *MemoryConsolidationReceipt {
		if i >= 0 {
			j.Steps[i].Status = "blocked"
			j.Steps[i].Error = err.Error()
		}
		out := s.consolidationReceipt(ctx, space, indexPath, opID, j, "partial", false)
		out.Code = code
		bytes, _ := json.Marshal(j)
		_ = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhasePending, string(bytes))
		return out
	}
	// Reconcile every committed child first, including a lost index checkpoint.
	for i := range j.Steps {
		_, err := s.proveConsolidationStep(ctx, a, space, j, i)
		if err != nil {
			return partial(i, err, "completion_unknown"), nil
		}
	}
	indexApplied := func() bool {
		for _, step := range j.Steps {
			if step.Kind == "index" && step.Status == "applied" {
				return true
			}
		}
		return false
	}
	for i := range j.Steps {
		step := &j.Steps[i]
		if step.Status == "applied" {
			continue
		}
		if step.Status == "blocked" {
			return partial(i, domain.Conflict(step.Error), "conflict"), nil
		}
		if err := s.checkConsolidationDependencies(ctx, a, space, j, "", indexApplied()); err != nil {
			return partial(-1, err, "source_changed"), nil
		}
		want := consolidationWanted(j, i)
		if step.Kind == "target" && j.Plan.Targets[i].ExpectedRevision == "" {
			meta, err := s.CreateNote(ctx, a, space, step.Path, want, step.ID, false)
			if err != nil {
				return partial(i, err, "conflict"), nil
			}
			step.DocumentID = meta.ID
		} else {
			expected := j.IndexRevision
			if step.Kind == "target" {
				expected = j.Plan.Targets[i].ExpectedRevision
			}
			previous, err := s.Docs.Revision(ctx, expected)
			if err != nil {
				return partial(i, err, "completion_unknown"), nil
			}
			if previous == nil {
				return partial(i, domain.Conflict("base revision unavailable"), "conflict"), nil
			}
			_, err = s.SaveContent(ctx, a, space, step.DocumentID, expected, want, step.ID, previous.SHA256)
			if err != nil {
				return partial(i, err, "conflict"), nil
			}
		}
		if _, err := s.proveConsolidationStep(ctx, a, space, j, i); err != nil {
			return partial(i, err, "completion_unknown"), nil
		}
		bytes, _ := json.Marshal(j)
		if err := s.Docs.SetOperationPhase(ctx, opID, domain.OpPhasePending, string(bytes)); err != nil {
			out := s.consolidationReceipt(ctx, space, indexPath, opID, j, "partial", false)
			out.Code = "completion_unknown"
			return out, nil
		}
	}
	// Serialize final verification/publication against normal vault mutations.
	// Earlier steps remain individually committed; this is not multi-file atomicity.
	finalLock := s.spaceMu(space)
	finalLock.Lock()
	defer finalLock.Unlock()
	if err := s.checkConsolidationDependencies(ctx, a, space, j, "", indexApplied()); err != nil {
		return partial(-1, err, "source_changed"), nil
	}
	for i := range j.Steps {
		if _, err := s.proveConsolidationStep(ctx, a, space, j, i); err != nil {
			return partial(i, err, "conflict"), nil
		}
	}
	outcome := "applied"
	if len(j.Steps) == 0 {
		outcome = "unchanged"
	}
	out := s.consolidationReceipt(ctx, space, indexPath, opID, j, outcome, true)
	j.Receipt = out
	bytes, _ := json.Marshal(j)
	var err error
	if committer, ok := s.Docs.(ports.MemoryOperationCommitter); ok {
		err = committer.CommitMemoryOperation(ctx, space, opID, string(bytes))
	} else {
		err = s.Docs.SetOperationPhase(ctx, opID, domain.OpPhaseCommitted, string(bytes))
		if err == nil {
			_, err = s.Docs.AppendEvent(ctx, &domain.Event{SpaceID: space, OperationID: opID, EventType: "memory.changed", CreatedAt: time.Now()})
		}
	}
	if err != nil {
		out = s.consolidationReceipt(ctx, space, indexPath, opID, j, "partial", false)
		out.Code = "completion_unknown"
		return out, nil
	}
	return out, nil
}
