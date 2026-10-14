---
title: Project Lifecycle
created: 2026-09-29
updated: 2026-10-12
type: concept
tags: [roadmap, review, testing]
sources: [raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# Project Lifecycle

Requirements arrive as `requirements-phase[N]` folders at repo root (**quote the `#` in shells; specs are read-only**). Delivery evidence lands in `review/phase-N/`.

## Process per phase
1. Read spec (`README`, theme docs like `UI-REVIEW.md`, `GRAPH.md`)
2. Decompose into ordered packages (`review/phase-N/PLAN.md`), track in `STATUS.md`
3. Per package: implement → gates ([[go-backend]] + [[frontend-app]]) → deploy [[deployment]] → live browser verification → `result.md` evidence, read back
4. Independent CPA validation review (see `review/cr2`, phase-2 `validation/REVIEW.md`)

## Current state (2026-10-12)
- **Phase 2:** W01–W03 closed; W04 **backend** done ([[links-and-graph]]); W04 frontend folded into phase-3
- **Phase 3:** accent system ([[accent-system]]), compact explorer/tabs/density, graph backend + graph UI — delivered and live-verified
- **Phase 4 / agent interface:** the machine-facing data interface (capability discovery, budgeted context, change feed) and memory governance are implemented — see [[agent-interface]] and `review/agent-memory/PLAN.md`
- **Identity & multi-user:** OIDC single sign-on + external identity ([[integrations-oauth]]) with roles, sharing, invitations, and an admin surface ([[roles-sharing-admin]])
- **Graph & notes surface:** a lightweight full link index (`graph2`), queryable memos, and a tags endpoint alongside the legacy graph and v1 analysis — see [[graph-v2-and-memos]]
- **Contract:** the OpenAPI 3.1 document is generated from the live route table ([[http-api]])
- Gap: `requirements-phase#3` lacks `IMPLEMENTATION-AND-ACCEPTANCE.md`/`STATUS.md`; per-requirement `Pass:` lines are binding
- Workspace rework history: `review/WORKSPACE-REWORK-20260926.md`, `workspace-checkpoints/`

## Related
[[deployment]] · [[links-and-graph]] · [[accent-system]] · [[agent-interface]]
