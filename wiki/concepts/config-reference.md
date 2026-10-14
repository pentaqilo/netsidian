---
title: Config Reference
created: 2026-10-12
updated: 2026-10-12
type: concept
tags: [config, deployment, auth]
sources: [raw/notes/code-scan-2026-10-12.md]
confidence: high
---

# Config Reference

The full `NETSIDIAN_*` environment contract from `config/config.go`. It is
**portable** — it describes *how* an instance is configured, not any particular
host, cluster, or address (those live in deployment configs, not the wiki —
see [[deployment]]).

## Runtime
`NETSIDIAN_BIND` (default `127.0.0.1`), `NETSIDIAN_PORT`, `NETSIDIAN_PUBLIC_URL`,
`NETSIDIAN_TRUSTED_PROXY`, `NETSIDIAN_LOG_LEVEL`, `NETSIDIAN_INSTANCE` (`server`;
set by the desktop build).

## Auth ([[auth-modes]], [[integrations-oauth]])
`NETSIDIAN_AUTH_MODE` = `none | local | external | mixed` (`none` refuses a
non-loopback bind). `NETSIDIAN_SIGNUP_ENABLED`. OIDC: `NETSIDIAN_OIDC_ISSUER`,
`_CLIENT_ID`, `_CLIENT_SECRET`, `_LABEL`, `_ALLOWED_CLAIMS`, `_ADMIN_CLAIMS`,
`_ALLOW_ALL_USERS`, `_EMAIL_CLAIM`, `_NAME_CLAIM`, `_EMAIL_VERIFIED_CLAIM`,
`_SCOPES`.

## State & storage ([[persistence]])
`NETSIDIAN_STATE_DIR`, `NETSIDIAN_STORAGE_ROOT`, `NETSIDIAN_STORAGE_MODE`
(`managed`), `NETSIDIAN_STORAGE_DRIVER` (`local | s3`),
`NETSIDIAN_STORAGE_ALLOWED_ENDPOINTS`, `NETSIDIAN_STORAGE_TRUSTED_STS_ENDPOINTS`,
`NETSIDIAN_S3_BUCKET | _REGION | _ENDPOINT | _PREFIX | _PATH_STYLE`,
`NETSIDIAN_DATABASE_DRIVER` (`sqlite`), `NETSIDIAN_SQLITE_PATH`
(`<state>/database/netsidian.db`), `NETSIDIAN_SQLITE_BUSY_TIMEOUT_MS`.

## Limits
`NETSIDIAN_MAX_NOTE_BYTES` (2 MiB), `NETSIDIAN_MAX_ATTACHMENT_BYTES` (25 MiB),
`NETSIDIAN_REVISION_RETENTION_DAYS` (30), `NETSIDIAN_TRASH_RETENTION_DAYS` (30).

## Local folder attach
`NETSIDIAN_LOCAL_FOLDER_PICK` gates the `/local/*` routes ([[http-api]]).
Sensitive boolean flags are parsed through `securityBool` (fail-closed)
^[raw/notes/code-scan-2026-10-12.md].

## Related
[[deployment]] · [[persistence]] · [[auth-modes]] · [[http-api]]
