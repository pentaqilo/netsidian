---
title: Frontend App
created: 2026-09-29
updated: 2026-09-29
type: entity
tags: [frontend, ui, editor]
sources: [raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# Frontend App

`src/frontend/` — Vite + React + TypeScript SPA embedded into the Go binary (`src/dist` via Dockerfile build stage).

## Structure
- Pages: `Shell`, `Workspace` (explorer + tabs + editor + graph side pane), `DocPage`, `GraphPage`, `SearchPage`, `MemoriesPage`, `SettingsPage`, `AuthPage`
- Components: `FileTree`, `NoteEditor`, `Editor` (CodeMirror source), `VisualEditor` (Milkdown `@milkdown/kit` 7.22.2 WYSIWYG with a `visualSafe()` data-loss guard and a formatting toolbar: block styles H1–H6, quote/code-block+language/hr, bold/italic/strike/inline-code/clear — note Milkdown mark names are `emphasis`/`strike_through`/`inlineCode` — list toggles with in-place type conversion (`convertList`), task items, indent/outdent, block move, validated link dialog (`expandMark`/`linkHrefAllowed`), GFM table operations, source-mode switch), `GraphView`, `Appearance`, `Dialog`, `Icon`
- Lib: `lib/markdown.ts` (rendering, missing-link titles), `lib/accent.ts` ([[accent-system]]), `lib/drafts.ts` (localStorage drafts), `lib/graphModel.ts`

## Build & test contracts
- Gates: `npx tsc --noEmit && npx vitest run && npx vite build`, run in `node:22-alpine` with a node_modules cache volume (host has no local node env)
- `tsconfig.json` types: `["vite/client", "node"]` (node needed for `process.env` in `vite.config.ts`, `NETSIDIAN_DEV_API`)
- Vitest with jsdom; test files colocated (`*.test.ts`)

## UI direction
Compact-controls policy (no permanent bulk-action bars, fixed tab strip, 40–44px graph toolbar) drives phase-3 rework — see [[project-lifecycle]] and `requirements-phase#3/UI-REVIEW.md`.

## Media, diagrams & math (2026-09-30, CPA plan)

- `lib/diagrams.ts` — fence dispatch for `mermaid|plantuml|puml`: engines load lazily (dynamic import; plantuml 3.6 MB / mermaid 640 KB / viz split chunks), rendered SVG is cached per source+theme, and output is sanitized. Sanitization is split deliberately: PlantUML output goes through DOMPurify (`svg`+`svgFilters` profiles), Mermaid goes through a structural scrubber (`scrubSvg`) because mermaid v12 puts node labels in `foreignObject` and DOMPurify 3.x strips its children — `securityLevel:'strict'` already escapes labels. Theme adaptation: mermaid `theme:'base'` + themeVariables; PlantUML gets skinparam injection (`ArrowColor`/`ArrowFontColor`/transparent background — `defaultFontColor` must NOT be injected, it whitens participant boxes).
- `lib/markdown.ts` — KaTeX math rules (`math_inline` via `.before('escape')`, `math_block`) with Obsidian-style guards so "$5 and $10" stays literal; `markdown-it-footnote`; katex css bundled.
- `lib/images.ts` + NoteEditor paste/drop handlers — clipboard/dropped files upload via `PUT /attachments/{path}` (name `<note-basename>-N.ext`, dedupe counter) and insert embeds; server-side content-type sniffing already existed.
- `components/TextFileEditor.tsx` — preview/edit panel for `.puml/.mmd/.svg` attachments; Workspace routes tree clicks to it, and the panel yields to note routes (`textFile && !activeId`, navigation clears it). Save = `PUT attachments` with If-Match (409 on stale).
- Graph edge contrast: whole-vault + local graph edges raised to `#9a9a9a` (0.65) / membership dotted 0.30 for dark-theme legibility.
- VisualEditor diagram widget note: ProseMirror mounts widgets after the factory returns — first render must be deferred (`setTimeout 0`) or the `isConnected` guard skips it.

## Related
[[markdown-pipeline]] · [[accent-system]] · [[http-api]]
