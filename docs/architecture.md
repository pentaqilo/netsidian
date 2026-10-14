# Architecture

Netsidian is a **hexagonal (ports & adapters)** Go core with two transports and
one embedded single-page app. Everything durable is plain files + SQLite in a
single binary — no external database, no SaaS.

## Layers

```
transport (httpapi │ wails native)  →  service  →  ports (interfaces)  →  sqlite + content store
                                          ↑
                              config · domain types (shared)
```

- **`internal/domain/`** — ids, models, path rules, the permission matrix
  (`RoleCan`), scope constants, typed errors. **No I/O.** This is the source of
  truth for what a role/scope may do.
- **`internal/ports/`** — capability interfaces the services depend on. Services
  never import adapters directly.
- **`internal/service/`** — all business logic: documents, revisions, spaces,
  identity, graph analysis, search, memos, the agent interface. This is where
  validation, caps, and idempotency live — and where the big behavioral test
  suites run.
- **`internal/repository/sqlite/`** and **`internal/storage/local/`** (and
  `storage/providers/` for S3) — the adapters that implement the ports.
- **`internal/markdown/`** — the server-side link parser, mirrored by the
  frontend `lib/` renderer so server and client agree on links.
- **`internal/transport/httpapi/`** — REST `/api/v1`, sessions, CSRF, SSE, and
  the generated OpenAPI. **`internal/transport/wails`** — the native desktop
  bridge.
- **`internal/bootstrap/`** — the single composition root. Build tags select
  `server_mode.go` (HTTP) vs `native_mode.go` (Wails v3).

## Invariants

- **Content bytes never enter SQLite; metadata never lives in files.** Notes and
  attachments are real files under the vault's content folder; SQLite holds
  identity, revisions, the link index, accounts, and the op journal.
- **The SPA talks only to `/api/v1`.** Desktop and web share the exact same
  service layer and API — the desktop just embeds the built `frontend/dist`.
- **Server behavior is testable without a browser.** Service suites drive
  journeys, safety, and idempotency; prefer extending them over HTTP-level tests.
- **Access is computed, not stored per request.** Effective permission =
  role ∩ key-scope ∩ key vault-binding, re-evaluated on every request (and, for
  SSO users, against freshly validated claims).
- **Every write is revisioned and replay-safe.** Immutable revisions + an op
  journal make retries safe and history restorable.

## Adding a capability

1. Declare it on a **port** first.
2. Implement the **adapter** second.
3. Wire it in **`bootstrap`**.
4. Keep handlers thin; put rules (validation, caps, idempotency) in services.

No cross-package imports into adapters; the service layer depends only on ports.

## Frontend

React 19 + Vite 7 (`frontend/`): `pages/`, `components/`, and `lib/` with
colocated tests. Pure, domain-free logic belongs in `lib/*.ts` (see
`lib/accent.ts` as the template). The three editors (CodeMirror source, Milkdown
visual, read view) all operate on the same Markdown file; the visual editor
refuses documents it cannot round-trip losslessly.

## Where to go deeper

The contributor **[wiki](../wiki/index.md)** is the living, cross-linked map of
this codebase (start at `concepts/architecture.md`). The
[API reference](api.md) documents the transport contract; the
[OpenAPI spec](../internal/transport/httpapi/openapi.yaml) is the machine-readable
source of truth.

**See also:** [development.md](development.md) · [api.md](api.md) ·
[data-model.md](data-model.md) · [../wiki/index.md](../wiki/index.md)
