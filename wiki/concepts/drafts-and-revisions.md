---
title: Drafts & Revisions
created: 2026-09-29
updated: 2026-09-29
type: concept
tags: [drafts, storage]
sources: [raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# Drafts & Revisions

Two-tier editing model: optimistic browser drafts, server-authoritative revisions.

## Draft tier (browser)
- `src/frontend/src/lib/drafts.ts` — localStorage per document, ~750ms debounce autosave, survives reload; reconciled against server revision on open ([[frontend-app]])
- Lost if webview storage cleared — by design, only *unsaved* work

## Revision tier (server)
- Every save creates an immutable revision (full body) — `service/documents.go` + `internal/storage/local` ([[persistence]])
- **Idempotent writes**: client op-id recorded in the op journal (`journal/ops`); retries don't duplicate revisions (safety-tested in `safety_test.go`)
- Conflict: save carries base revision; stale base → conflict response, client offers merge/reopen
- Restore-from-revision and history feed (`history/`, `events.go` SSE) back the History panel ([[http-api]])

## Contracts
- Save with unchanged body → no new revision
- Draft present + newer server revision → draft-wins-vs-stale-base rules resolved in UI per workspace tests (`workspace_test.go`)

## Related
[[markdown-pipeline]] · [[vaults-and-spaces]] · [[frontend-app]]
