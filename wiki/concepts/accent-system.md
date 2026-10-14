---
title: Accent System
created: 2026-09-29
updated: 2026-09-29
type: concept
tags: [ui, frontend]
sources: [raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# Accent System

Theme accent engine delivered in phase-3 P3-A01/A02 (requirement A03 ongoing) — see [[project-lifecycle]].

## Model — `src/frontend/src/lib/accent.ts` (pure, unit-tested)
- Presets: `'default' | 'neutral' | 'custom'`; `effectivePreset()` migrates legacy prefs (old hex→custom, absent→default)
- Default violet `#b7a0ff` (dark) / `#6840bd` (light); neutral `#dadada` / `#414141`
- WCAG contrast gate: custom colors must hit **4.5:1** against bg (`#1e1e1e` dark / `#ffffff` light), measured by relative luminance; failing colors are mixed 5% steps toward the theme foreground (e.g. `#222222`→`#858585` at 4.52:1)

## Surfaces — `styles.css` + `components/Appearance.tsx`
- Tokens `--accent-soft/-border/-focus` via `color-mix`; accent marks focus/active only — `.file-row.selected` deliberately neutral (A03 direction)
- Applied to links, h2, blockquote/callout borders, mark tint, missing-link dashed style ([[markdown-pipeline]])
- Persisted via workspace state ([[http-api]]); survives reload; theme-aware

## Related
[[frontend-app]] · [[project-lifecycle]] · [[drafts-and-revisions]]
