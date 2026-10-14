---
title: Go Backend
created: 2026-09-29
updated: 2026-09-29
type: entity
tags: [architecture, api, storage]
sources: [raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# Go Backend

The `src/` Go module (`github.com/pentaqilo/netsidian`) implementing the whole application core in both run modes (see [[architecture]]).

## Key files
- `src/main.go` — entry point; mode selected by build tags
- `src/internal/bootstrap/` — `bootstrap.go` wiring + `server_mode.go` (HTTP) / `native_mode.go` (Wails, `//go:build !server`) — see [[desktop-app]]
- `src/internal/config/config.go` — env resolution, state-dir rules ([[persistence]])
- `src/internal/domain/` — ids, models, paths, errors
- `src/internal/ports/` — `ports.go` interfaces + `analysis.go` (graph-analysis contracts) — the hexagonal boundary ([[architecture]])
- `src/internal/service/` — documents, folders, spaces, identity, vault_export, workspace_state, graph_analysis, search, memory, settings, attachments, events, health
- `src/internal/markdown/parse.go` — server-side link parsing ([[links-and-graph]], [[markdown-pipeline]])
- `src/internal/repository/` — `sqlite/` (primary), `postgres/` (see [[sqlite-vs-postgres]])
- `src/internal/storage/local/` — content-addressed note/attachment store on disk ([[vaults-and-spaces]])
- `src/internal/transport/httpapi/` — [[http-api]]
- `src/migrations/` — SQL migrations (sqlite dir)

## Build & test contracts
- All server code is behind `-tags server`; native build without it
- Gates before any deploy: `go vet -tags server ./...` and `go test -tags server ./... -count=1` (~55s, run in `golang:1.27-alpine` with a Go module cache volume)
- Service-level suites: `phase2_w01/w02/w04`, `journey`, `safety`, `local_vault`, `workspace`, `independent_r2/r3/cpa`, `phase4_review`

## Related
[[http-api]] · [[persistence]] · [[deployment]]
