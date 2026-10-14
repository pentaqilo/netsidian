---
title: Persistence & State
created: 2026-09-29
updated: 2026-10-12
type: entity
tags: [storage, config]
sources: [raw/notes/state-dir-resolution.md, raw/notes/code-scan-2026-10-12.md]
confidence: high
---

# Persistence & State

Three cooperating layers: SQLite (metadata), local content store (bytes), op journal (idempotency).

## State root resolution
`NETSIDIAN_STATE_DIR` > `$XDG_STATE_HOME/netsidian` > `~/.local/state/netsidian` — on Windows this is likewise `%USERPROFILE%\.local\state\netsidian`, deliberately **not** `%APPDATA%`. ^[raw/notes/state-dir-resolution.md]

```
<state>/content/spaces/<spaceID>/...   # note bodies + attachments (NETSIDIAN_STORAGE_ROOT overrides root)
<state>/database/netsidian.db          # SQLite (NETSIDIAN_SQLITE_PATH override)
<state>/journal/ops                    # op journal → idempotent writes ([[drafts-and-revisions]])
<state>/history/                       # history feed entries
<state>/secrets/auth_secret            # session signing secret ([[auth-modes]])
```

## Implementation
- `src/internal/repository/sqlite/` — single primary DB, migrations in `src/migrations/sqlite/`
- `src/internal/storage/local/` — content store keyed per space ([[vaults-and-spaces]])
- Postgres repository exists but is migration-target only ([[sqlite-vs-postgres]])
- Browser drafts live in **localStorage**, not the state dir ([[frontend-app]], [[drafts-and-revisions]])

## Storage providers
`NETSIDIAN_STORAGE_DRIVER` selects `local` (default) or `s3` (S3-compatible:
`NETSIDIAN_S3_BUCKET|REGION|ENDPOINT|PREFIX|PATH_STYLE`, with
`NETSIDIAN_STORAGE_ALLOWED_ENDPOINTS` / `_TRUSTED_STS_ENDPOINTS` as the
allow-list) ^[raw/notes/code-scan-2026-10-12.md]. A space can be **bound** to a
provider via `GET/PUT /spaces/{space}/storage`, and `GET /storage/providers`
lists what the instance offers; the public capabilities contract reports
`folderPicker: none | native | server` ([[roles-sharing-admin]], [[config-reference]]).

## Related
[[go-backend]] · [[desktop-app]] · [[deployment]] · [[config-reference]]
