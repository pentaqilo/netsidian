# Implementation state

Updated: 2026-10-22. Latest release: Netsidian 0.5.0.

## Implemented

- Shared zoom and minimap navigation for Maps, Graph and task boards.
- Task-board hand panning, temporary Space-drag, empty-space dragging and middle-button panning. Navigation does not change task status or files.
- Map inline editing, resizing, connector selection/reconnection/deletion, edge auto-pan, and double-click preview without camera zoom changes.
- Larger map card text with matching edit/read typography.
- Compact Memos, live tags, calendar sidebar resizing and visual/source composer; removed linking tips and fixed ordinary wiki-link label/deletion handling.
- File explorer creation menu for documents, folders, memos, boards and maps; specialized icons and single-click quick-open controls.
- Boards/Maps grouped by parent folder with search, folder filters and actual document activity dates.
- N14 branding and application-accent integration.

## Also completed

- Settings → Interaction: document defaults, reading double-click action, map
  preview gesture, inline editing, memo composer mode and initial minimap visibility.
- Reading mode defaults with per-open-document explicit mode choices.
- Internal map membership metadata hidden in rendered/visual views and preserved
  on save; code examples and unrelated comments remain ordinary content.
- Preference write serialization prevents Appearance/Interaction changes from
  overwriting each other in one application session.

## Release

- Published: [Netsidian 0.3.0](https://github.com/pentaqilo/netsidian/releases/tag/0.3.0).
- Release commit: `096b47c8` (merged and pushed to `main`).
- [GitHub release workflow](https://github.com/pentaqilo/netsidian/actions/runs/39505626373): completed successfully.
- Four desktop archives, Helm chart, `docker-image.txt` and `SHA256SUMS` published.
- Docker: `ghcr.io/pentaqilo/netsidian:0.3.0`, multi-platform digest
  `sha256:15522a8f3d2b7931645c6fe612a93195a58e8a28908aa873c026f7ec542a3599`.
- Public checksum and image-digest files downloaded and verified as available.
- Production-container smoke test passed; no implementation/release blockers remain.

## MCP design backlog

Current MCP functionality remains supported and audited. Proposed typed knowledge/project tools, bounded chunk reads, project context, task claims and company service identities are documented in `mcp-agent-workflows.md`; these proposed APIs are not implemented by this release.

## Validation

- Complete Go server internal test suite passed in Docker.
- Go static analysis passed in Docker.
- Clean Docker frontend suite: 44 files / 378 tests passed.
- Production TypeScript check and Vite bundle passed; embedded Go server built.
- MCP/HTTP version integration tests passed after unifying the reported version.
- Playwright: map card typography and inline size stability; dark/light N14 icons;
  normal memo wiki-link editing without false errors; board hand scroll/Escape;
  grouped/filterable lists at desktop and 390px mobile with no horizontal overflow;
  persisted interaction settings, double-click mode switching, metadata round-trip,
  map preview/inline options and default minimaps across Maps, Graph and Boards.

Production review image: `netsidian:0.3.0-review`
(`sha256:0ef46dd990d8697ba62d100a8f31aef3f14e69412690374716caa068208ee8ca`).
Playwright confirmed Reading defaults, double-click Visual editing, hidden metadata,
hand scroll (335px), minimap and reported version 0.3.0 on the compiled container.
Review endpoint: http://127.0.0.1:3010. Existing review data volume retained.

Release notes: `.github/release-notes/0.3.0.md`. GitHub publication uses the
repository's tag-triggered desktop/Docker/Helm workflow.

## Delegation and review

OpenCode used its configured default agent and reported
`qwen3.8-flash-next-iq3_xxs`. Codex independently reviewed the delegated changes,
completed integration and fixed metadata/code-example preservation, preference
write ordering, exact configuration-file matching, folder action spacing,
chronological activity dates and default minimaps in retained hidden tabs.
The temporary Vite process was stopped after production Docker validation.

## Unreleased follow-up: detach confirmation

- Detaching a native/attached-folder vault uses a Yes/No dialog with no name input.
- Yes sends the current vault name through the existing API contract; No sends no
  detach request. Draft flushing and dirty-state guards remain in place.
- Managed permanent deletion retains typed-name confirmation. Attached-folder
  actions are labelled Detach vault.
- OpenCode default agent (`qwen3.8-flash-next-iq3_xxs`) supplied the initial change;
  independent review corrected the button labels to literal Yes/No.
- Validation: TypeScript passed; 378 frontend tests passed for the delegated
  change; Playwright checked no input, both buttons, cancelled/no request,
  confirmed/automatic name, and the separate permanent-deletion name field.
  All detach requests were intercepted; no real vault was detached.

## Unreleased follow-up: MCP reader and memory reliability

- Added direct document reads by path, actionable ID/path guidance and safe
  read-only mind-map configuration-file aliases. Mutations keep canonical folders.
- Connection/discovery report credential-specific vault operations, read-only state
  and memory write availability, without granting additional permissions.
- Fixed data loss from missing/wrong content-update fields: missing/null `markdown`
  returns 400 without creating a revision. Explicit empty markdown remains valid.
- Documented vault IDs, create/update payload differences, verbatim preferences and
  read-back verification before claiming persistence.
- OpenCode performed initial research; Codex completed and independently reviewed
  implementation. Regression coverage includes scoped credentials, cross-vault
  denial, path aliases and the exact preference-wipe scenario.
- Final validation: complete `go test -tags server -count=1 ./internal/...` and
  `go vet -tags server ./internal/...` passed in Docker; `git diff --check` passed.
- Local review image `netsidian:mcp-usability-review` is running at
  http://127.0.0.1:3010 with the existing review volume retained. Playwright API
  smoke checks returned 200 for document-by-path and both canonical/config-file
  map reads, with identical canonical map paths. No user documents were modified.
- Changes remain local and unreleased.

## Unreleased: focused MCP tools and indexed memory

- Added six focused search/document/memory tools alongside existing REST adapters.
  New inputs consistently use vaultId/documentId/revision/markdown and reject
  unknown fields. An omitted vault resolves only with one accessible vault.
- Memory uses an ordinary Markdown index with stable keys, exact short text,
  scope and links. Long details live separately; existing project resources are
  referenced rather than copied. Configured/custom memory folders are supported.
- Independent review fixed response-budget accounting, read-only no-op writes,
  missing-key collisions, unsafe detail overwrite, exact text serialization,
  stale document chunks and index pagination. Idempotent receipts prevent replay
  from reapplying old preferences after newer writes.
- Compact authenticated connection output and shorter descriptions reduce
  repeated setup/context overhead. Full setup remains available with setup=true.
- Updated the portable agent skill in Settings to prefer focused tools and
  indexed memory. Existing notes are retained without automatic migration.
- OpenCode default agent (qwen3.8-flash-next-iq3_xxs) supplied the initial service
  and MCP implementation; Codex completed fixes and independent regression tests.
- Validation: full backend suite and vet passed in Docker; final focused service
  and MCP tests passed after follow-up fixes. Coverage includes unrelated-entry
  and human-text preservation, exact text, replay after newer edits, changed-key
  rejection, stale revisions, UTF-8/escaped JSON budgets, missing refs, board/map
  config IDs, custom folders, strict unknown fields, read-only/write-only keys
  and ambiguous vault selection. The portable-skill test and production frontend
  build (including TypeScript) passed. git diff --check passed.
- Measured test responses: verified preference receipt 149 JSON bytes;
  authenticated connect approximately 1.1 KiB; tool definitions approximately
  11 KiB total for all 11 authenticated tools (including legacy adapters).
- Review image: netsidian:mcp-index-review at http://127.0.0.1:3010; existing
  review data volume retained. Changes remain local, uncommitted and unreleased.
- Playwright smoke checks on the running final image returned HTTP 200 for the
  app and an authenticated document read. No live user memory or notes were
  written; mutation scenarios used isolated disposable test vaults.

## Release 0.4.0

The follow-ups above are included in 0.4.0; their earlier local/unreleased notes
record the state before this release. Version metadata and release notes have
been prepared. Final validation and GitHub publication are in progress.

Release validation passed: 378 frontend tests (44 files), complete backend tests,
server vet, strict Helm lint and diff whitespace checks. Production frontend
build and Docker review smoke checks passed before the metadata version bump.
Publication uses the existing Release workflow triggered by tag 0.4.0.

### Published 0.4.0

- Published: https://github.com/pentaqilo/netsidian/releases/tag/0.4.0.
- Release commit/tag: 8699d44 / 0.4.0, pushed to main and GitHub.
- Workflow 39680936443 succeeded: four desktop targets, Helm, multi-platform
  Docker and release publication. All seven expected assets are present.
- SHA256SUMS entries match GitHub's asset digests; the downloaded checksum
  manifest and Docker digest file were independently verified.
- Docker: ghcr.io/pentaqilo/netsidian:0.4.0, immutable multi-platform digest
  sha256:4090c7e5e8bfd9865d98cf5b4f8fd3fcf7d26de28127acd1792949601bcfe445.
- Screenshot refresh requested after publication is a separate documentation
  follow-up; the published 0.4.0 tag is retained unchanged.

### Screenshot follow-up

- Refreshed README appearance, document reading mode, Memos, Kanban preview and
  map screenshots; added MCP/agent settings. Six real 1440x1000 captures from
  published 0.4.0, with accurate captions and all 11 README image links checked.
- OpenCode's initial attempt was stopped after excessive graph framing; the
  screenshot subagent completed captures and root independently reviewed images,
  catching and correcting a transient Memos loading state before publication.
- Automatic approval rejected creating a persistent demonstration vault in the
  shared review instance. The approved safer alternative used isolated tmpfs
  test data in a temporary Docker container; container/data were removed after
  capture. Shared review content was retained; scratch files were removed.
- Published 0.4.0 image now runs on the retained local review container at
  http://127.0.0.1:3010. The screenshot documentation follow-up does not move or
  replace release tag 0.4.0.

## Follow-up: internal memory references and project-work proposal

- Branch: fix/memory-internal-links. Fixed stable-ID link resolution in reading
  surfaces and graph/backlinks, including legacy bare detail/refs IDs without
  changing saved Markdown. Reading links use current document titles when known.
- Regression tests reproduce the previous missing backlink and unclickable
  reference; cover exact source preservation, literal IDs in text/code, rename
  and missing-ID protection. No real user documents were modified.
- OpenCode default agent supplied the backend contribution and initial resolver
  extension; independent integration completed UI metadata wiring and corrected
  missing-ID fallback and structured-field handling.
- Analyzed project wiki + board + map workflows in docs/mcp-agent-workflows.md.
  Proposed project_prepare/context/record, task_create/move and map_link tools
  reuse current services. These project workflow tools remain a proposal and
  are not implemented by this memory-link fix.
- Final validation passed: complete backend suite, server vet, 382 frontend
  tests across 45 files, production frontend build and git diff --check.
  The validated memory-link fix is tracked on fix/memory-internal-links;
  published 0.4.0 and its review container have not been changed by this fix.

## Follow-up: file-list refresh button

- Added a circular-arrow Refresh file list button to the explorer action row.
  Reuses the current-vault document/folder/attachment loader; remains available
  for read-only vaults, disables during a manual refresh and re-enables on failure.
- OpenCode default agent (qwen3.8-flash-next-iq3_xxs) implemented the change;
  independent review retained filters, selection and editor drafts and removed
  incidental whitespace changes.
- Validation: TypeScript and all 382 existing frontend tests passed. An isolated
  Playwright browser confirmed tooltip/visible placement, one refresh request,
  pending disabled state, preserved filter and recovery from a simulated 500.
  Browser checks made no vault writes. Temporary preview/browser were stopped.
- Local changes only; no new push or release for this follow-up.

## Main-branch consolidation (2026-10-22)

The user requested all completed work on main, removal of other branches, and
no new branches unless explicitly requested. The validated memory-link fix,
project-workflow design and file-list refresh button are consolidated together.
Earlier branch/local-only status entries above are historical and superseded by
this consolidation. AGENTS.md records the standing main-only workflow preference.

## MCP access and creation UX (2026-10-22)

- Redesigned MCP & agents into connection, token creation, token management and
  expandable memory sections. A generated token replaces its own form in place,
  with copy/dismiss controls and scoped access/expiry details. Creation/list errors
  stay local; a list refresh failure never discards the one-time secret. Read-only
  access remains the default; write and trash permissions are explicit choices.
- New documents open in the Visual Markdown Editor with their first heading
  selected; existing heading-driven automatic filename updates remain in place.
- Board creation starts with a selected title and derives the folder until the
  user explicitly edits the location. Existing column/template controls remain.
- Added a cryptographically random UUID fallback for HTTP origins and applied it
  to board/map/memo/memory-folder creation. No credential generation changed.
- Default accent is #70a4fb in both themes. Explicit custom and blue presets remain
  available; primary button text adapts to the accent for readable contrast.
- Validation: 388 frontend tests passed; TypeScript and production build passed
  (existing bundle-size warning). Playwright verified desktop/mobile token creation
  and dismissal with a mocked API, read-only grants, no mobile horizontal overflow
  and no browser errors. An in-memory editor check confirmed selected title
  replacement produces the expected Markdown heading. No real tokens or vault
  documents were created.
- OpenCode delegation was attempted but its configured API was unavailable; the
  implementation and UX review were completed directly. Changes remain on main;
  no branch, push or release requested for this work.

## Release 0.5.0 (2026-10-22)

The consolidated main-branch work above (memory internal-link fix, file-list
refresh button, MCP access redesign and creation UX) is included in 0.5.0.
Version metadata (Go constant, frontend package/lock, Helm chart, workflow
default) was bumped and custom release notes were prepared in
.github/release-notes/0.5.0.md.

### Published 0.5.0

- Published: https://github.com/pentaqilo/netsidian/releases/tag/0.5.0.
- Release commit/tag: 4977d86 / 0.5.0, pushed to main and GitHub.
- Workflow 39777404810 succeeded: four desktop targets (with frontend tests and
  backend service tests on the Linux job), Helm packaging with strict lint,
  multi-platform Docker publish and release publication. All seven expected
  assets are present: four desktop archives, netsidian-0.5.0.tgz chart,
  docker-image.txt and SHA256SUMS.
- Custom release notes from .github/release-notes/0.5.0.md were used verbatim.
- Docker: ghcr.io/pentaqilo/netsidian:0.5.0, immutable multi-platform digest
  sha256:8a2714669638d1006b37c456281ea835530341db27593985d2081e9fbde04226.
- Local Go toolchain was unavailable for a pre-tag build; the release workflow
  ran the full backend and frontend test suites on its Linux job before
  packaging, and the version bump touched only constant/metadata strings.

### Screenshot follow-up (0.5.0)

- Refreshed all 12 README screenshots from the published 0.5.0 image: graph link
  layout, appearance, document reading mode, Memos, Kanban, mind map, MCP/agent
  settings, PDF viewer, file explorer, empty vault, the sign-in screen with the
  MCP instructions panel, and the graph topic atlas. Twelve real 1440x1000
  captures with accurate captions and all 12 README image links checked. The
  captures show the redesigned default accent (#70a4fb), the new Refresh file
  list button, and the PDF Convert to Markdown action.
- Captures used isolated tmpfs test data in a temporary Docker container
  (netsidian-shots, ghcr.io/pentaqilo/netsidian:0.5.0, host port 3011) seeded via
  the REST API: one Demo vault with notes, a Kanban board, a mind map with a
  linked note and connectors, memos, a knowledge map and one PDF attachment. The
  container and its tmpfs data were removed after capture; no shared/review
  content was touched and no real tokens were created.
- Graph follow-up: renamed the graph view's visible title from "Knowledge map" to
  "Graph" (Graph2.tsx; the section aria-label already read "Graph"). The hero
  knowledge-map.png now shows a denser link layout (30 notes, 138 links) and a new
  topic-atlas.png shows the atlas layout with folder regions and the knowledge-base
  sidebar. Because the rename is not in the published 0.5.0 image, these two
  captures came from a source-built image (netsidian:shots) in the same isolated
  tmpfs container, seeded with 30 cross-linked notes across six folders; the
  container and its data were removed afterwards.
