---
title: Markdown Pipeline
created: 2026-09-29
updated: 2026-09-29
type: entity
tags: [markdown, graph, editor]
sources: [raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# Markdown Pipeline

Two independent parsers must agree on link semantics.

## Server side — `src/internal/markdown/parse.go`
- Extracts wikilinks (double-bracket targets with optional alias) and markdown links from note bodies
- Feeds the link index used by backlinks and both graph endpoints ([[links-and-graph]])
- Resolution rules: basename or path match, `.md` suffix enforced; code spans/fences are excluded from extraction (verified by `phase2_w04_test.go`)
- Unresolved targets materialize as nodes `u:<lowercased-path-with-.md>`

## Client side — `src/frontend/src/lib/markdown.ts`
- Read-mode rendering for `markdown-preview` surfaces; callouts, marks, tasks
- Missing wikilink targets render as dashed accent links with a `title` attribute (phase-3 P3-A02 work, see [[accent-system]])
- Tests colocated (`markdown.test.ts`)

## Editors
- Source: CodeMirror (`components/Editor.tsx`)
- Read: rendered `markdown.ts` output
- Visual: Milkdown round-trip editor with `visualSafe()` refusal on unsafe round-trips ([[frontend-app]])

## Related
[[links-and-graph]] · [[drafts-and-revisions]] · [[go-backend]]
