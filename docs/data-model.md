# Data model

What Netsidian stores and how the pieces relate. The golden rule: **notes and
attachments are plain files; SQLite holds everything else** (identity,
revisions, the link index, accounts, journal).

## Vaults (spaces)

A **vault** (internally a *space*, id `spc_…`) is an isolated notebook: its own
folders, documents, attachments, link index, tags, and settings. Nothing leaks
across vaults — folder and document ids are space-scoped.

- **Personal** vs **shared** kind. Every account gets a personal vault.
- **Lifecycle**: create → rename (`PUT /spaces/{space}`) → archive/unarchive →
  delete (owner, typed name confirmation).
- **Workspace state** (`GET/PUT /spaces/{space}/workspace`) is per-user open-tab
  layout, not content.
- **Export** (`GET /spaces/{space}/export`) streams a portable ZIP of the vault.
- **Identity survives** export/import round-trips (the vault identity file is
  preserved).

On disk a vault is a real, Obsidian-portable folder tree:
`<state>/content/spaces/<spaceID>/…` of `.md` files + attachments.

## Documents

A **document** (id `doc_…`) is a Markdown note with a vault-relative `path`.
`DocMeta.kind` ∈ `note` | `memory` | `attachment`.

- **Memory is not a special store** — it is an ordinary `kind: memory` Markdown
  document in a chosen folder, with the same tags, links, graph, revisions,
  sharing, and storage as any note. There are **no dedicated `/memories`
  routes**; manage it through the normal document API.
- **Plain text files** (non-`.md`) are handled by `…/text-files/{path}`.
- **Memos** are documents tagged `memo` in their frontmatter; the
  `…/memos` endpoint paginates them by creation time with folder/tag/text/date
  filters.

## Revisions, drafts & idempotency

- **Every save is an immutable revision.** `DocMeta.currentRevision` is the
  current one; `…/revisions` lists history and `…/revisions/{rev}` reads a past
  version. Restore any version.
- **Browser drafts** autosave (~750 ms) to **localStorage** (not the server)
  with base-revision conflict detection; reload-safe in that browser only.
- **Optimistic concurrency**: guarded writes require `If-Match: "<revision>"`
  (missing → 428, stale → 412). Never blind-overwrite.
- **Idempotency**: an `Idempotency-Key` makes a create/update replay-safe; the
  op journal (`<state>/journal/ops`) backs safe retries.

## Folders, attachments & trash

- **Folders** are real directories; rename moves a subtree; trash/restore work
  on subtrees. Folder-attached vaults (local/native folder pick) recycle nothing.
- **Attachments** are binary files stored alongside notes (≤ 25 MiB default),
  uploaded as `application/octet-stream`.
- **Trash** has a retention horizon (`NETSIDIAN_TRASH_RETENTION_DAYS`, default
  30) before permanent purge (owner-only).

## Links, backlinks & graph

- `[[wikilinks]]` (with `|alias` and `folder/Note` forms) and standard Markdown
  links are indexed by the server link parser (`internal/markdown/`), mirrored by
  the client renderer. Links inside code fences/inline code are ignored; matching
  is case-insensitive with `.md` implied.
- **Backlinks** come from the link index (`…/documents/{doc}/backlinks`).
- **Graph surfaces**:
  - `…/graph` — legacy link graph.
  - `…/graph/analysis` — scoped analysis (scope/direction/depth, 1000-node cap,
    `truncated` flag).
  - `…/graph2` — the complete lightweight index (all notes + unresolved nodes,
    deduped links, no community analysis or sampling) used by the continuous
    Graph 2 canvas.
- Unresolved link targets appear as `unresolved` nodes so the graph stays honest
  about gaps.

## Tags

Tags are extracted from frontmatter and body `#tags` into a `document_tags`
index. `…/tags` returns vault tag counts; search and memos filter by tag with
`subtree` (nested children) or `exact` matching.

## Storage providers

- **local** (default): content lives in the state dir.
- **s3**: `NETSIDIAN_STORAGE_DRIVER=s3` stores Markdown, attachments, folder
  markers, and trash as S3 objects under a dedicated prefix. Existing local
  vaults are **not** silently migrated; folder-attached vaults stay on their
  filesystem. A vault can be bound to a provider via `…/storage`.
- SQLite, immutable revision history, and the op journal **always** need a
  persistent state volume, even with S3 content.

## State layout

```
<state>/
├── content/spaces/<spaceID>/   # your notes + attachments (portable)
├── database/netsidian.db       # SQLite (WAL): metadata, revisions, accounts, link index, sessions
├── journal/ops                 # idempotency journal for save retries
├── history/                    # immutable revision bytes
└── secrets/auth_secret         # OAuth-flow + provider-credential secret (see authentication.md)
```

State root: `NETSIDIAN_STATE_DIR` > `$XDG_STATE_HOME/netsidian` >
`~/.local/state/netsidian`. Back up `<state>` as one unit. Full env reference:
[configuration.md](configuration.md).

**See also:** [api.md](api.md) · [architecture.md](architecture.md) ·
[authentication.md](authentication.md) · [configuration.md](configuration.md)
