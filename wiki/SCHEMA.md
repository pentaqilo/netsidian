# Wiki Schema

## Domain
Netsidian — architecture, behavior, and operations of this repository: the Go backend, React frontend, desktop (Wails) and server (HTTP/Helm) run modes, persistence, and the delivery process (requirements phases, review evidence, deployments). Not general tech notes; everything here describes *this* project.

## Conventions
- File names: lowercase, hyphens, no spaces (e.g. `links-and-graph.md`)
- Every wiki page starts with YAML frontmatter (see below)
- Use `[[wikilinks]]` to link between pages (minimum 2 outbound links per page)
- When updating a page, bump the `updated` date
- Every new page must be added to `index.md` under the correct section
- Every action must be appended to `log.md`
- **Provenance:** code is the primary source of truth; pages cite concrete repo paths (backticks) and, for snapshots/probes, `^[raw/notes/...]` markers. If code and a page disagree, code wins — update the page.

## Frontmatter
```yaml
---
title: Page Title
created: YYYY-MM-DD
updated: YYYY-MM-DD
type: entity | concept | comparison | query | summary
tags: [from taxonomy below]
sources: [raw/notes/source-file.md]
confidence: high | medium | low
---
```
`confidence: high` only for claims verified against code or a live system in the same era of the repo; design intent or phase-spec claims that await implementation stay `medium`.

## Tag Taxonomy
- Architecture: architecture, api, storage, frontend, markdown, graph, editor, desktop
- Behavior: auth, identity, sharing, vault, drafts, ui, config, memory
- Delivery: deployment, testing, roadmap, review

Every tag on a page must appear here. New tag → add here first, then use it.

## Page Thresholds
- **Create a page** for a subsystem, cross-cutting concept, or decision that spans 2+ files/phases
- **Add to existing page** when work touches something already covered
- **DON'T create a page** for a single function, one-off bugfix, or passing mention — it belongs on the subsystem page
- **Split a page** past ~200 lines; **archive** fully superseded pages to `_archive/`

## Page Content Requirements
- Entity pages (subsystems): what it is, key files/paths, behavior contracts, how to run/test it, related pages
- Concept pages: definition, current state, invariants/contracts, open questions, related
- Comparisons: dimensions table + verdict

## Update Policy
Requirements phases change behavior. When a new phase contradicts an older page:
1. Newer implementation/evidence supersedes older notes; bump `updated`
2. If genuinely unresolved (spec vs code), keep both claims with dates, mark `contested: true`, flag in lint
