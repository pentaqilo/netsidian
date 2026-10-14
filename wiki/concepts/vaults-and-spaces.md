---
title: Vaults & Spaces
created: 2026-09-29
updated: 2026-10-12
type: concept
tags: [vault, storage, auth]
sources: [raw/notes/state-dir-resolution.md, raw/notes/live-probe-2026-09-29.md, raw/notes/code-scan-2026-10-12.md]
confidence: high
---

# Vaults & Spaces

A **space** is a vault: one isolated notebook with its own folders, documents, link index, and content directory.

## Model
- `service/spaces.go` + `identity.go`: space create/list, vault identity file (phase-2 C01: identity survives export/import roundtrip)
- Content: `<state>/content/spaces/<spaceID>/…` — a real, Obsidian-portable folder tree of `.md` files + attachments ([[persistence]], implemented by `storage/local` in [[go-backend]])
- Export: `vault_export.go` + `GET /spaces/{space}/export` (zip) via [[http-api]]
- Lifecycle: `PUT /spaces/{space}` renames; `POST /spaces/{space}/archive|unarchive` soft-retires a vault; `GET/PUT /spaces/{space}/workspace` persists the open-tab layout ^[raw/notes/code-scan-2026-10-12.md]
- IDs: `spc_…` snowflake-ish; documents `doc_…`. Fresh reseeds of the local instance mint new ids each time ^[raw/notes/live-probe-2026-09-29.md]

## Sharing & roles
A vault is shared by granting a **role** (`owner` / `member`) on it — never by
copying content. Members and invitations are managed under
`/spaces/{space}/members` and `/spaces/{space}/invitations`
([[roles-sharing-admin]]). The agent interface surfaces exactly the vaults a
principal may touch, with their `allowedOperations` ([[agent-interface]]).

## Verification fixtures
The local seed script creates two vaults (`main`, `side`) — two-space isolation is how cross-space tests and the side-by-side graph pane ([[links-and-graph]]) get exercised.

## Invariants
- Space operations never leak across spaces (folder/doc ids are space-scoped)
- Deleting a space removes DB rows; content dir removal follows the op journal ([[drafts-and-revisions]])

## Related
[[persistence]] · [[links-and-graph]] · [[deployment]] · [[roles-sharing-admin]]
