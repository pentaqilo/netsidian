---
title: HTTP API
created: 2026-09-29
updated: 2026-10-12
type: entity
tags: [api, auth]
sources: [raw/notes/repo-structure-2026-09-29.md, raw/notes/live-probe-2026-09-29.md]
confidence: high
---

# HTTP API

`src/internal/transport/httpapi/` — the server-mode transport. `server.go`
registers every route through a `doc()` helper that also records the route for
the generated OpenAPI document. Base path `/api/v1`.

## Route surface (grouped)
- **Auth**: `POST /auth/setup|login|signup|logout`, `GET /me`; OIDC `GET /auth/oauth/providers` (+ `/start`, `/callback` when configured) — [[integrations-oauth]], [[auth-modes]]
- **Self-service**: `GET/PUT /me/preferences`, `GET/POST /me/keys` + `DELETE /me/keys/{id}` (API keys / PATs), `GET /me/integrations`
- **Admin** (operator-only): `GET /admin/settings`, `GET /admin/users`, `PATCH /admin/users/{id}`, `GET /admin/audit` — [[roles-sharing-admin]]
- **Spaces**: `GET/POST /spaces`, `GET/PUT/DELETE /spaces/{space}` (PUT renames), `POST /spaces/{space}/archive|unarchive`, `GET/PUT /spaces/{space}/workspace`, `GET /spaces/{space}/export` (zip) — [[vaults-and-spaces]]
- **Sharing**: `POST /spaces/{space}/invitations`, `DELETE .../invitations/{id}`, `POST /invitations/accept`, `GET /spaces/{space}/sharing/users`, `POST /spaces/{space}/members`, `PUT/DELETE .../members/{member}` — [[roles-sharing-admin]]
- **Storage**: `GET /storage/providers`, `GET/PUT /spaces/{space}/storage` (per-space provider binding) — [[persistence]]
- **Folders**: `GET/POST /spaces/{space}/folders`, `POST .../folders/rename|trash|restore`, `GET .../folders/trash`
- **Documents**: `GET/POST /spaces/{space}/documents`, `GET /documents/{doc}`, `PUT /documents/{doc}/content`, `POST .../rename|trash|restore`, `GET .../revisions` + `/revisions/{rev}`, `GET .../backlinks` — [[drafts-and-revisions]], [[links-and-graph]]
- **Attachments / text files**: `GET /spaces/{space}/attachments`, `PUT/GET .../attachments/{path...}`, `GET/PUT .../text-files/{path...}` (non-`.md`)
- **Retrieval & graph**: `GET /spaces/{space}/search`, `GET .../graph` (legacy), `GET .../graph/analysis` (v1), `GET .../graph2` (lightweight full index), `GET .../memos`, `GET .../tags` — [[graph-v2-and-memos]]
- **Agent data interface**: `GET /agent/whoami`, `POST /spaces/{space}/agent/context`, `GET /spaces/{space}/changes/since` — [[agent-interface]]
- **Native / local (conditional)**: `GET /native/status`, `POST /native/vaults/attach` (desktop only); `GET /local/status|/local/folders`, `POST /local/folders`, `POST /local/vaults` (only when `NETSIDIAN_LOCAL_FOLDER_PICK=1`) — [[config-reference]]
- **Ops / contract**: `GET /health` (+ `/healthz`, `/livez`), `GET /settings` (public before setup), `GET /openapi.json` / `/openapi.yaml`, `GET /spaces/{space}/events` (SSE)

## Contracts
- **Auth**: session cookie `nsd_session` (NOT bearer). Anonymous `/me` → 401. Modes `none|local|external|mixed` — [[auth-modes]]
- **CSRF gate**: every mutation requires header `X-Netsidian-Requested: 1` and a same-origin `Origin` (or non-`cross-site` `Sec-Fetch-Site`). Bearer-key callers are cookie-free and exempt
- **Limits**: body cap 32 MiB (1 MiB on pre-auth endpoints); a per-principal burst limiter (burst 40, refill 10/s, cap 60) guards mutations, and a separate limiter guards the agent surface ([[agent-interface]])
- **Analysis v1** ([[links-and-graph]]): `scope` (`vault|folder|note|selection`), `direction` (`both|in|out`), `depth` (0–3; default 0 for vault/folder, 1 for note/selection), `query`, `limit` (≤1000). Responses flag `truncated` when the 1000-node cap is hit; unresolved targets surface as synthetic nodes. Legacy `GET /graph` stays unchanged
- **Memory** has **no dedicated routes** — it is a `kind: memory` document edited through the ordinary document API ([[agent-interface]])
- **OpenAPI 3.1** is generated from the live route table at startup; native/local routes appear only when enabled. `openapi.yaml` supplies annotations and `openapi_test.go` fails on any undocumented route
- Errors are typed `domain` errors mapped to a JSON envelope `{error:{code,message,retryable}}` (`code` ∈ invalid, unauthenticated, forbidden, not_found, conflict, precondition_failed, precondition_required, too_large, unavailable, rate_limited, unsupported)

## Related
[[go-backend]] · [[auth-modes]] · [[agent-interface]] · [[integrations-oauth]]
