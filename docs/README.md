# Documentation

Netsidian is a local-first, Obsidian-style Markdown workspace — browser, server,
or native desktop — backed by a single Go binary + SQLite, with your notes kept
as plain `.md` files.

## Start here

| If you want to… | Read |
|---|---|
| Run it for the first time | [getting-started.md](getting-started.md) |
| Install it (Docker / Compose / Kubernetes / desktop / source) | [installation.md](installation.md) |
| Bring content into the new namespaces | [migration.md](migration.md) |
| Understand every option & the security model | [configuration.md](configuration.md) |
| Expose it safely (TLS, backups, Kubernetes) | [deployment.md](deployment.md) |
| Hack on the code | [development.md](development.md) |
| Fix a problem | [troubleshooting.md](troubleshooting.md) |

## Concepts & reference

| Topic | Guide |
|---|---|
| How the system is built (hexagonal core, transports, invariants) | [architecture.md](architecture.md) |
| What is stored and how it relates (vaults, documents, revisions, links, storage) | [data-model.md](data-model.md) |
| Auth modes, sessions, CSRF, API keys, OIDC | [authentication.md](authentication.md) |
| Back up & restore a whole instance (storage-aware, verified) | [backup.md](backup.md) |
| Document-backed mind maps, branching and linked files | [mindmaps.md](mindmaps.md) |
| PDF tabs, conversion, file drops and recoverable trash | [file-workflows.md](file-workflows.md) |
| **The full HTTP API** (REST + agent interface) | [api.md](api.md) |
| Using Netsidian from an agent (PATs, memory-as-Markdown, workflow) | [agents.md](agents.md) |

## Machine-readable contract

The live, always-in-sync API contract is generated from the route table:

- `GET /api/v1/openapi.json`
- `GET /api/v1/openapi.yaml`
- source: [`../internal/transport/httpapi/openapi.yaml`](../internal/transport/httpapi/openapi.yaml)

If a guide and the OpenAPI spec disagree, **the spec (and the code) wins**.

## For contributors

The **[wiki](../wiki/index.md)** is the living, cross-linked architecture map
(start at `concepts/architecture.md`). It is kept current with the code and is
the place to record structural decisions.

---

*Your notes are just files — Netsidian only ever adds folders it clearly owns.*

- [Folder-backed Kanban boards](kanban.md) — file layout, metadata, permissions and move semantics.
