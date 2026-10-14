---
title: SQLite vs Postgres
created: 2026-09-29
updated: 2026-09-29
type: comparison
tags: [storage, architecture]
sources: [raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# SQLite vs Postgres

Both adapters implement the same [[architecture|ports]]; SQLite is what actually ships and is tested.

| Dimension | SQLite (`repository/sqlite`) | Postgres (`repository/postgres`) |
|---|---|---|
| Status | **Primary**, all suites run against it | Migration target; not wired for production runs |
| Deployment | Zero-ops file DB in every mode ([[deployment]]) | Would need external service |
| Migrations | `src/migrations/sqlite/` applied at boot | README placeholder only |
| Concurrency | Single-writer WAL; fits local-first | Relevant only for future multi-user server |
| Test coverage | Full (`go test -tags server ./...`) | Compile/shape only |

**Verdict:** treat Postgres as future work — never add features relying on PG-only capabilities; keep SQL in the port boundary so the adapter stays swappable ([[persistence]], [[go-backend]]).
