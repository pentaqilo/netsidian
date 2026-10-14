# Migrations

- SQLite migrations are embedded in `internal/repository/sqlite/migrations/` so
  the single-binary release always ships the exact schema it was built with.
  This deviates from the earlier proposed top-level layout; see the project
  report. `migrations/sqlite/` here would otherwise duplicate them.
- PostgreSQL migrations (planned, not yet implemented — SQLite is the shipped
  engine in this build) will live in `internal/repository/postgres/migrations/`
  under the same embedded rule, with separate reviewed SQL per engine.
