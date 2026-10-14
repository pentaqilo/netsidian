---
title: Architecture
created: 2026-09-29
updated: 2026-10-12
type: concept
tags: [architecture, api]
sources: [raw/notes/repo-structure-2026-09-29.md, raw/notes/code-scan-2026-10-12.md]
confidence: high
---

# Architecture

Hexagonal (ports & adapters) Go core with two transports and one embedded SPA.

## Layers
```
transport (httpapi | wails)  →  service  →  ports (interfaces)  →  sqlite / storage.local
                                  ↑ config, domain types shared
```
- `internal/ports/ports.go` (+`analysis.go`) declares every capability services may use; only `repository/` and `storage/` implement them ([[persistence]])
- `bootstrap/bootstrap.go` is the single composition root; build tags pick `server_mode.go` vs `native_mode.go` ([[desktop-app]])
- `domain/` holds ids, models, path rules, typed errors — no I/O
- HTTP handlers stay thin; business rules (idempotency, caps, validation) live in services ([[http-api]])
- A governed **agent data interface** (capability discovery, budgeted context, change feed, memory proposals) sits on the same service layer — no bypass of roles or key scopes ([[agent-interface]])

## Invariants
- Server behaviors are testable without a browser: service suites drive everything ([[go-backend]])
- The SPA talks only to `/api/v1` — desktop and web share it ([[frontend-app]])
- Content bytes never enter SQLite; metadata never lives in files ([[persistence]])
- Effective access = role ∩ key-scope ∩ per-vault binding, re-checked per request for external identities ([[roles-sharing-admin]], [[integrations-oauth]])

## Open questions
- Postgres adapter completion ([[sqlite-vs-postgres]])
- Graph-analysis v2 (communities/group edges) still planned; the lightweight full link index `graph2` is already delivered alongside it ([[links-and-graph]], [[graph-v2-and-memos]])

## Related
[[go-backend]] · [[http-api]] · [[desktop-app]] · [[agent-interface]]
