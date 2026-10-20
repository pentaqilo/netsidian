# Implementation state

Updated: 2026-10-21. Target release: Netsidian 0.3.0.

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

- Target: 0.3.0. Production-container smoke test passed. GitHub publication pending.

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
