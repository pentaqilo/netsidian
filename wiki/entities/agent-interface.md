---
title: Agent Interface
created: 2026-10-12
updated: 2026-10-12
type: entity
tags: [api, architecture, memory]
sources: [raw/notes/code-scan-2026-10-12.md]
confidence: high
---

# Agent Interface

A machine-facing data interface over the governed service layer, so an LLM agent
can read and write a vault without bypassing permissions. Contract string
`netsidian-agent-v2` (identical for REST and MCP) ^[raw/notes/code-scan-2026-10-12.md].
**Memory is ordinary Markdown** — a `kind: memory` document in a chosen folder —
not a special store (see *Memory* below).

## Endpoints ([[http-api]])
- `GET /agent/whoami` — capability discovery: `{contract, principal, auth{method, keyId, keyLabel, scopes, bindVaults, readOnly}, vaults[{id, name, role, allowedOperations}], tools[]}`. Effective capabilities = **role ∩ key-scope ∩ per-vault binding**; `readOnly` when no write op is allowed. Tools: documents, search, graph-analysis, context, changes-since
- `POST /spaces/{space}/agent/context` — one deterministic, byte-budgeted retrieval pass. Request: `query` **or** `folder` **or** `tags` required; `kinds(note|memory)`, `updatedAfter`, `includeGraph` (default false; requires `graph:read`), `graphDepth` (default 1, max 2), `maxResults` (default 8, max 20), `snippetChars` (default 240, max 600), `maxBytes` (default 8192, max 131072 — the **hard** budget; `maxTokens` is only a hint), `cursor`, `excludeDocumentIds`. Responses respect `maxBytes` for the complete encoded JSON; a budget too small for one result is rejected. The opaque cursor is bound to the vault + index generation (a cross-scope cursor → 409)
- `GET /spaces/{space}/changes/since` — durable per-vault change-feed replay of `changed` / `tombstones` with a vault-bound `cursor` (`limit` 1..500)

## Memory (retired dedicated workflow)
Dedicated `/memories` routes and the review/proposal UI are **removed**. Context
no longer queries separate memory tables, and `includeMemories=true` is rejected.
New HTTP tokens **cannot** request memory scopes. A memory is simply a
`kind: memory` Markdown document with the same tags, links, graph, revisions,
sharing, and storage as any note — read, search, move, and edit it through the
ordinary document API. Legacy records/backing Markdown are preserved in their
original folders for durable-state compatibility ^[raw/notes/code-scan-2026-10-12.md].

## Rate limiting
The agent retrieval surface has its own per-principal limiter (`agent:<userID>`), separate from the mutation burst limiter — reads are throttled too ^[raw/notes/code-scan-2026-10-12.md].

## Related
[[http-api]] · [[architecture]] · [[drafts-and-revisions]] · [[roles-sharing-admin]]

## Portable evidence-backed memory

Canonical policy: `skills/agent-memory/SKILL.md` and its local/remote/Netsidian
adapters. Run `python scripts/generate-memory-policy.py --check` to verify the
Go/frontend bindings, and `python scripts/test-local-memory.py` for local CAS
and confinement tests. Memory reasoning runs in the client agent. The service
enforces ordinary vault permissions, revisions and persistence receipts; a
folder/scope identity does not grant access. See
[architecture decision](../../docs/agent-memory-architecture.md).
