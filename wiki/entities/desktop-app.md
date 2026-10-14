---
title: Desktop App (Wails)
created: 2026-09-29
updated: 2026-09-29
type: entity
tags: [desktop, architecture, storage]
sources: [raw/notes/state-dir-resolution.md]
confidence: high
---

# Desktop App (Wails)

Same Go core as the server, embedded in a native window via **Wails v3**. Selected by build tags: `native_mode.go` compiles when `server` tag is absent ([[architecture]]).

## Facts
- Entry wiring: `src/internal/bootstrap/native_mode.go`; bind/port env vars are server-mode concepts and unused natively
- Build helper: `src/Dockerfile.desktop-build`, tasks in `src/Taskfile.yaml`
- **Persistence identical to server mode** — state root resolution and layout per [[persistence]]:
  Linux `$XDG_STATE_HOME`/`~/.local/state/netsidian`; Windows `%USERPROFILE%\.local\state\netsidian` (not `%APPDATA%`)
- Webview localStorage holds drafts ([[drafts-and-revisions]]) — clearing webview data loses unsaved drafts but never committed revisions
- Auth in desktop default: `none` mode over loopback (refuses non-loopback bind) — [[auth-modes]]

## Related
[[persistence]] · [[go-backend]] · [[frontend-app]]
