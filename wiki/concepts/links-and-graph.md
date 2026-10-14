---
title: Links & Graph
created: 2026-09-29
updated: 2026-09-29
type: concept
tags: [graph, markdown]
sources: [raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# Links & Graph

Link extraction ([[markdown-pipeline]]) feeds a link index that powers backlinks, the legacy graph, and the v1 analysis endpoint.

## Endpoints ([[http-api]])
- `GET /spaces/{space}/graph` — legacy whole-vault graph, kept stable
- `GET /spaces/{space}/graph/analysis` — v1 contract (`service/graph_analysis.go`):
  - params: `depth` (absent→-1 full; folder-scope default 1; 0–3 valid), `direction` in/out/both, `scope` (note|folder), `path` seed
  - caps: hard 1000 nodes **including the seed** (returned==limit), hard edge cap; `matched` counts are pre-cap over the full index
  - unresolved targets → node id `u:<lowercased-path-with-.md>` with evidence path/revision
  - envelope: `version:1`, `status:"ready"`, `truncated`, `reason:"node-limit"`
  - folder boundary is exact path segments (`Research` ≠ `Research-old`); code-literal links excluded

## Client
- `lib/graphModel.ts` builds render models; `GraphView.tsx` renders the workspace side pane and GraphPage ([[frontend-app]])

## Planned (phase 3, P3-G01–G08 — not yet implemented)
- Deterministic communities (label propagation), folder grouping, group-edge aggregation, shortest paths
- Cytoscape compound+cose UI, overview mode when scoped docs >80 **or** explicit edges >4× doc count, inspector, saved views
- See [[project-lifecycle]] for the package order

## Related
[[markdown-pipeline]] · [[vaults-and-spaces]] · [[http-api]]
