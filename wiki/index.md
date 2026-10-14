# Wiki Index

> Content catalog for Netsidian project documentation.
> Read this first to find relevant pages for any query.
> Last updated: 2026-10-12 | Total pages: 20

## Entities

- [[go-backend]] — Go module layout, build tags, and test gates for the whole core
- [[http-api]] — `/api/v1` route surface, session-cookie auth, CSRF, analysis v1 + OpenAPI contract
- [[persistence]] — state-dir resolution, SQLite + content store + op journal, storage providers
- [[frontend-app]] — Vite/React SPA: pages, components, lib modules, node:20 gates
- [[markdown-pipeline]] — server link parser vs client renderer; editors
- [[desktop-app]] — Wails v3 native mode, persistence, default none-auth
- [[deployment]] — one image, local + Kubernetes targets, Helm, verify steps (env-agnostic)
- [[agent-interface]] — `netsidian-agent-v2`: whoami, budgeted context, change feed, memory proposals
- [[integrations-oauth]] — OIDC login flow, external identity, scoped API keys

## Concepts

- [[architecture]] — hexagonal ports/adapters, two transports, governed agent reads, shared invariants
- [[vaults-and-spaces]] — a space is a vault: identity, content dirs, lifecycle, sharing, ids
- [[drafts-and-revisions]] — localStorage drafts vs idempotent server revisions
- [[links-and-graph]] — link index, legacy graph, analysis v1 caps, phase-3 graph plan
- [[graph-v2-and-memos]] — lightweight full link index (`graph2`), memo query, tags
- [[auth-modes]] — `none | local | external | mixed`, sessions, CSRF header
- [[roles-sharing-admin]] — per-space roles, sharing/invitations, admin + audit, capabilities
- [[config-reference]] — the portable `NETSIDIAN_*` environment contract
- [[accent-system]] — presets, 4.5:1 contrast gate, color-mix tokens
- [[project-lifecycle]] — requirements phases, review evidence, current status

## Comparisons

- [[sqlite-vs-postgres]] — SQLite ships; Postgres is migration-target only

## Queries

*(none filed yet)*
