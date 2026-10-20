# Netsidian migration plan and delivered decisions

This document records how the reviewed working tree was migrated into the
Netsidian repository on the `feat/netsidian-migration` branch, and which
decisions were delivered. The source was imported as a reviewed snapshot;
the destination repository retains its own initial Git history.

## Scope and ground rules

- Source: the reviewed tracked working tree of the predecessor repository
  (clean `main`, latest commit `71c8373`, including the TOTP concurrency fix
  `d908771` and the corrected SQLite `_txlock=immediate` behavior).
- Destination: this repository, branch `feat/netsidian-migration`, preserving
  its existing `.git` history and its `LICENSE` file unchanged.
- Only tracked source files were copied (392 files, plus the preserved
  `LICENSE`). `.git`, generated builds, `node_modules`, credentials, state
  directories, and local caches were never copied.
- Final delivery commits the reviewed snapshot on the migration branch,
  merges it into `main`, and pushes a new `0.1.0` release tag. The release
  workflow gates publication on desktop builds, tests, image publication,
  and Helm packaging.

## Rebranding decisions

Ordered replacements were applied across all intended tracked text and
filenames:

| Area | Old form | New form |
| --- | --- | --- |
| Product name | legacy monogram/wordmark | `NETSIDIAN` / `Netsidian` / `netsidian` |
| Go module | legacy module path | `github.com/pentaqilo/netsidian` |
| Environment variables | legacy prefixed variables | `NETSIDIAN_*` |
| Cookies | legacy session/oauth names | `nsd_session`, `nsd_oauth` |
| Personal access tokens | legacy key form | `nsd_<keyid>_<secret>` |
| MCP server and tools | legacy tool prefix | server `netsidian`, tools `netsidian_*` |
| Backup archive format | legacy format id | `netsidian-backup/v1` |
| Vault export manifest | legacy folder | `netsidian/vault.json` |
| Protected vault folders | legacy hidden folders | `.netsidian`, `.netsidian-trash` |
| State directory and DB | legacy names | `netsidian-state`, `netsidian.db` |
| Binary | legacy binary | `netsidian` |
| Container image | legacy registry path | `ghcr.io/pentaqilo/netsidian` |
| Helm chart directory | legacy chart name | `helm/netsidian` |
| Drag-and-drop MIME type | legacy MIME | `application/netsidian-document` |
| Browser storage keys | legacy prefixes | `netsidian:...` |
| Custom DOM events | legacy event names | `netsidian-native-pinch`, `netsidian:missing-link` |
| Internal headers | legacy headers | `X-Netsidian-Requested`, `X-Netsidian-MCP-Origin` |

Filenames renamed: `helm/` chart directory and the static icon
`frontend/public/icons/netsidian.svg`. Obsolete icon assets
(`c-10-light.svg`, `c-10-dark.svg`, `w-08-light.svg`, `w-08-dark.svg`,
`02-rotated.svg`) were removed.

## Logo decision

The current monogram uses the supplied `n-14-dark.svg` / `n-14-light.svg` geometry,
retained under `frontend/public/icons/`, and defined for theme/accent-aware inline use in
`frontend/src/lib/brand.ts`:

- `BRAND_BODY = M30 58 86 26V195L30 228Z M172 79 228 46V197L172 230Z`
- `BRAND_ACCENT = M86 26 172 133V230L86 123Z`

The same geometry is used for the favicon and the static
`frontend/public/icons/netsidian.svg`. It was verified in both light and dark
themes.

## Versioning decision

The first public increment is `0.1.0`. It was applied consistently to
`frontend/package.json`, the root entries in `frontend/package-lock.json`,
`internal/service/settings.go`,
`helm/netsidian/Chart.yaml`, the release workflow default, README examples,
`docs/development.md`, `helm/README.md`, the OpenAPI `info.version`, and the
MCP implementation version. Inherited release notes for later versions were
removed; the only release notes file is `.github/release-notes/0.1.0.md`.

## Compatibility boundary

Fresh Netsidian namespaces replace the legacy ones outright; they are not
disguised aliases. This is a deliberate compatibility boundary:

- Session and OAuth cookie names and the personal access token prefix
  changed. Create new accounts, sessions and tokens in the fresh instance.
- The secret-sealing key derivation context changed. Existing encrypted
  storage profiles, OAuth credentials and TOTP secrets need recreation;
  copying or renaming an old database does not convert them.
- Backup archives and vault export manifests use the new format identifiers,
  so archives written by the predecessor are not silently accepted.
- Protected vault folders and the state directory use the new names.
- MCP tool names changed, so existing agent configurations must be updated.
- Start with a fresh state directory. Bring Markdown notes and supported
  attachments through the existing file import flow. If you exported a vault
  ZIP, extract it first: the current app does not import ZIP files directly.
  To preserve its directory layout, attach the extracted document folder
  through the desktop folder picker or an enabled local-folder provider.
  Recreate managed metadata and account settings in the new instance.
- Do not point the new instance at an unconverted predecessor state directory.
  Keep that instance and its verified backup available until the portable
  content has been checked in the new vault.

## Validation

Run in Docker (SELinux-safe mounts), never against live instances:

- Backend: `go vet -tags server ./...`, `go build -tags server`,
  `go test -tags server -count=1 ./internal/...` — 12 packages pass.
- Frontend: `npm ci`, `npx tsc --noEmit`, `npm test` (29 files, 236 tests),
  `npx vite build` — all pass.
- Independent review repeated the full backend and frontend gates, the TOTP
  concurrency/single-use tests 100 times, Windows desktop compilation, and
  Helm lint/render/package checks. OCR checks covered all nine screenshots;
  source, filenames and built server/Windows binaries were scanned for old
  branding. Dependency metadata and the license remained intact.
- Independent live checks confirmed the Netsidian page title, a successful
  login with the `nsd_session` cookie, and MCP initialization reporting
  server `netsidian` version `0.1.0`. Windows archive packaging also passed.
- A demo image `netsidian-demo:0.1.0` was built from this working tree and
  served on `127.0.0.1:3010` with an isolated state volume for review.

## Screenshots

All README screenshots were regenerated from the demo instance at
`1440x1000` and show the Netsidian branding: `knowledge-map.png`,
`appearance.png`, `document-view.png`, `memos.png`, `kanban.png`,
`pdf-viewer.png`, `files.png`, `empty-vault.png`, `login-mcp.png`.
Obsolete unreferenced screenshots were removed.

## Release automation

The numeric `0.1.0` tag starts `.github/workflows/release.yml` in this
repository. It builds Linux, Windows and macOS desktop archives, publishes
the amd64/arm64 image, packages the matching Helm chart, and creates the
GitHub release after those jobs succeed. Release downloads include checksums
and the immutable container digest.

Pushing the tag starts the workflow; successful asset publication must be
verified through authenticated GitHub access or the repository's Actions page.
