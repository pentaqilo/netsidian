# API reference

The Netsidian HTTP API. Base path **`/api/v1`**, JSON bodies, UTF-8. The
authoritative, always-in-sync contract is generated from the live route table
and served at:

- `GET /api/v1/openapi.json`
- `GET /api/v1/openapi.yaml`

This page is the human-readable companion to that spec. If this page and
`openapi.json` ever disagree, **the spec (and the code) wins**.

---

## 1. Conventions

### Authentication
Two schemes (both may be presented; a key wins over a cookie when both are present):

| Scheme | How | Notes |
|---|---|---|
| **Session cookie** | `Cookie: nsd_session=…` set by `/auth/login` (or OAuth) | Browser sessions. Mutations additionally need the CSRF header below. |
| **Bearer API key** | `Authorization: Bearer <token>` from `POST /me/keys` | Scoped personal access token (PAT). Cookie-free, so CSRF does not apply. Restricted by scopes + per-vault binding. |

`GET /api/v1/me` returns 401 when anonymous.

### CSRF guard
Every **non-GET** cookie-authenticated request must send `X-Netsidian-Requested: 1`
and be same-origin (`Origin` matches `Host`, or `Sec-Fetch-Site` is not
`cross-site`). Missing header → `403 forbidden` (`missing X-Netsidian-Requested
header`). Bearer-key callers are exempt.

### Optimistic concurrency (`If-Match`)
Content writes and trash require `If-Match: "<currentRevision>"`:

- missing on a guarded write → **428** `precondition_required`
- stale/mismatched → **412** `precondition_failed` (reread, merge, retry — never blind-overwrite)

The current revision is returned by `GET …/documents/{doc}` (also as the `ETag`)
and in `DocMeta.currentRevision`.

### Idempotency (`Idempotency-Key`)
`POST …/documents` and `PUT …/documents/{doc}/content` accept an optional
`Idempotency-Key` header:

- same key + same body → the original result (replay-safe)
- same key + different body → **409** `conflict`

Use a unique key per logical operation and keep it identical across retries.

### Rate limits
A per-principal token bucket guards mutations: **burst 40, refill 10/s, cap 60**.
Exceeding it → **429** `rate_limited` with `Retry-After`.

### Body size
32 MiB per request body (1 MiB on pre-auth endpoints). Note content is capped at
`NETSIDIAN_MAX_NOTE_BYTES` (default 2 MiB); attachments at
`NETSIDIAN_MAX_ATTACHMENT_BYTES` (default 25 MiB) → `413`-style `too_large`.

### Error envelope
All errors are JSON:

```json
{ "error": { "code": "forbidden", "message": "key lacks scope documents:write", "retryable": false } }
```

`code` ∈ `invalid`, `unauthenticated`, `forbidden`, `not_found`, `conflict`,
`precondition_failed`, `precondition_required`, `too_large`, `unavailable`,
`rate_limited`, `unsupported`.

### Roles & scopes
Access to a vault is **role ∩ key-scope ∩ key vault-binding**, re-checked per
request. Roles (per vault): **viewer**, **editor**, **owner**.

| Capability | viewer | editor | owner |
|---|:--:|:--:|:--:|
| read / search / export | ✅ | ✅ | ✅ |
| create / edit / attachments / trash / restore | — | ✅ | ✅ |
| purge / manage members / manage space | — | — | ✅ |

API keys carry **scopes** instead of a role. Grantable scopes:
`documents:read`, `documents:write`, `documents:delete`, `documents:purge`,
`attachments:read`, `attachments:write`, `search:read`, `graph:read`,
`history:read`, `history:restore`, `export:read`, `events:read`,
`members:manage`. (`memory:*` scopes are legacy and **not grantable to new
keys** — memory is now an ordinary document kind, see §7.) Administration and
member-management endpoints require a **human** (session) owner/operator — API
keys cannot use them.

---

## 2. System & ops

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/v1/health` · `/healthz` · `/livez` | public | readiness (`/livez` = process only; others 503 if a backing service is down) |
| GET | `/api/v1/settings` | public | instance settings + runtime capabilities (`InstanceSettings`) |
| GET | `/api/v1/openapi.json` · `/openapi.yaml` | public | live contract |
| GET | `/api/v1/native/status` | public | `{native: bool}` (desktop bridge) |
| POST | `/api/v1/native/vaults/attach` | session | attach a vault via the native bridge (desktop only) |
| GET | `/api/v1/local/status` | public | `{enabled: bool}` (only when `NETSIDIAN_LOCAL_FOLDER_PICK=1`) |
| GET/POST | `/api/v1/local/folders` | session | browse / register a pickable local folder |
| POST | `/api/v1/local/vaults` | session | attach a vault backed by a local folder |

`GET /settings` returns `InstanceSettings` (see §8) — auth mode, `needsSetup`,
`signupEnabled`, `version`, feature flags, and the runtime capabilities
(`kind`, `auth`, `storage`).

---

## 3. Auth

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/auth/setup` | public | create the **first operator** account (one-time) |
| POST | `/api/v1/auth/login` | public | password login → session cookie |
| POST | `/api/v1/auth/signup` | public | self-service signup (only when `signupEnabled`) |
| POST | `/api/v1/auth/logout` | session | end the session |
| POST | `/api/v1/auth/password` | session (human) | change own password; revokes **all** sessions and personal API keys |
| POST | `/api/v1/auth/challenge/totp` | challenge token | complete a pending login challenge with a TOTP code |
| POST | `/api/v1/auth/challenge/recovery` | challenge token | complete a pending login challenge with a recovery code |
| GET | `/api/v1/auth/oauth/providers` | public | list configured SSO providers |
| GET | `/api/v1/auth/oauth/start` | public | begin OIDC authorization-code + PKCE (302) |
| GET | `/api/v1/auth/oauth/callback` | public | verify provider identity, create session (303) |

`setup`/`login`/`signup` bodies are `Credentials` (`email`, `password` ≥ 8,
optional `displayName`). See [authentication.md](authentication.md) for modes,
sessions, and OIDC claim policy.

`/auth/password` takes `{currentPassword, newPassword}` (new password 10–1024
UTF-8 bytes, different from the current). It is allowed only for a signed-in
human on a local password account — API-key principals get 403 and
provider-managed accounts get 409. When the account has a confirmed second
factor, a valid current `code` is also required (step-up). On success the
session cookie is cleared and the response is `{ok, reauthRequired: true,
sessionsRevoked, keysRevoked}`: every session and personal API key of the
account is revoked atomically with the password rotation, so a stolen credential
cannot survive the change.

**Second factor (TOTP).** Local password accounts may enroll an optional
time-based code. `POST /auth/totp/enroll` (`{password}`) returns
`{secret, otpauthUrl, recoveryCodes[]}` once and leaves the factor *pending*;
`POST /auth/totp/confirm` (`{code}`) activates it. With a confirmed factor,
`POST /auth/login` returns `{challenge:true, challengeToken, userId,
displayName}` and **no** session cookie; the challenge expires after 5 minutes
and allows 5 attempts. `POST /auth/challenge/totp` or
`POST /auth/challenge/recovery` (`{challengeToken, code}`) then issues the
session — a pending challenge authorizes no other endpoint. `POST
/auth/totp/disable` and `POST /auth/totp/recovery-codes` both require
`{password, code}`. See [authentication.md](authentication.md).

---

## 4. Account & keys

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/v1/me` | session/key | current principal |
| GET | `/api/v1/me/totp` | session | second-factor status (`enabled`, `pending`, `recoveryCodesRemaining`) |
| POST | `/api/v1/auth/totp/enroll` | session (human) | start enrollment; secret + recovery codes shown **once** |
| POST | `/api/v1/auth/totp/confirm` | session (human) | confirm enrollment with a valid code |
| POST | `/api/v1/auth/totp/disable` | session (human) | disable the factor; password + current code |
| POST | `/api/v1/auth/totp/recovery-codes` | session (human) | regenerate recovery codes; password + current code |
| GET | `/api/v1/me/integrations` | operator | backend/OAuth diagnostics (no secrets) |
| GET/PUT | `/api/v1/me/preferences` | session | read / replace preferences |
| GET | `/api/v1/me/keys` | session | list my API keys (no secrets) |
| POST | `/api/v1/me/keys` | session | create a key; **secret returned once** |
| DELETE | `/api/v1/me/keys/{id}` | session | revoke a key |
| POST | `/api/v1/invitations/accept` | session | accept a vault invitation by `{token}` |

`POST /me/keys` body: `{label, spaceIds[], scopes[], expiresInDays}`. The
response includes the one-time secret; store it as `NETSIDIAN_TOKEN` and send it
as a bearer key. Revocation, role changes, and removal take effect on the next
request.

---

## 5. Administration (human operator only)

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/admin/settings` | effective auth + claim policy (no secrets) |
| GET | `/api/v1/admin/users` | search accounts (`q`, `offset`) |
| PATCH | `/api/v1/admin/users/{id}` | `{disabled, isOperator, revokeSessions}` — audited atomically |
| POST | `/api/v1/admin/users/{id}/totp-reset` | operator recovery: remove a member's second factor (audited `totp.reset`) |
| GET | `/api/v1/admin/audit` | account/role audit events (`offset`) |

Guards: you cannot disable/demote yourself or remove the last enabled local
administrator; provider-managed roles are protected (`409`). API keys cannot
reach these endpoints.

---

## 6. Vaults (spaces)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/v1/spaces` | member | vaults the caller belongs to (with roles) |
| POST | `/api/v1/spaces` | operator/owner | create a vault `{name, kind: shared\|personal, storage?}` |
| GET | `/api/v1/spaces/{space}` | member | vault detail |
| PUT | `/api/v1/spaces/{space}` | owner | rename `{name}` |
| DELETE | `/api/v1/spaces/{space}` | owner | delete — requires `{confirmation: "<name>"}` |
| POST | `/api/v1/spaces/{space}/archive` · `/unarchive` | owner | archive / unarchive |
| GET/PUT | `/api/v1/spaces/{space}/workspace` | member | per-user open-tab/workspace state |
| GET | `/api/v1/spaces/{space}/export` | read | stream a vault export archive (zip) |
| GET | `/api/v1/storage/providers` | member | available storage providers + default |
| GET/PUT | `/api/v1/spaces/{space}/storage` | human owner | read redacted / configure `VaultStorage` |
| POST | `/api/v1/spaces/{space}/invitations` | owner | invite `{email, role}` |
| DELETE | `/api/v1/spaces/{space}/invitations/{id}` | owner | revoke an invitation |
| GET | `/api/v1/spaces/{space}/sharing/users` | human owner | find provisioned users (`q`, 2–200 chars) |
| POST | `/api/v1/spaces/{space}/members` | human owner | share `{userId, role: viewer\|editor\|owner}` |
| PUT/DELETE | `/api/v1/spaces/{space}/members/{member}` | human owner | change role / remove |

Sharing grants a **role on the same vault** — it never copies content.
Federated (SSO) users become selectable after their first sign-in.

---

## 7. Documents, folders, attachments

### Documents

| Method | Path | Scope/role | Purpose |
|---|---|---|---|
| GET | `/api/v1/spaces/{space}/documents` | read | list metadata (`?trashed=true` for trash) → `{documents:[DocMeta], folders?, attachments?}` |
| POST | `/api/v1/spaces/{space}/documents` | create | create a note `{path, content}` (idempotent) |
| GET | `/api/v1/spaces/{space}/documents/{doc}` | read | read a note; `ETag` = revision |
| PUT | `/api/v1/spaces/{space}/documents/{doc}/content` | edit | save `{markdown}` with `If-Match` |
| POST | `/api/v1/spaces/{space}/documents/{doc}/rename` | edit | move/rename `{path}` |
| POST | `/api/v1/spaces/{space}/documents/{doc}/trash` | trash | move to trash (`If-Match` guarded) |
| POST | `/api/v1/spaces/{space}/documents/{doc}/restore` | restore | restore from trash |
| GET | `/api/v1/spaces/{space}/documents/{doc}/revisions` | read | revision history |
| GET | `/api/v1/spaces/{space}/documents/{doc}/revisions/{rev}` | read | read one historical revision |
| GET | `/api/v1/spaces/{space}/documents/{doc}/backlinks` | read | backlinks to a document |
| GET/PUT | `/api/v1/spaces/{space}/text-files/{path}` | read/edit | plain (non-`.md`) text file |

`DocMeta.kind` ∈ `note`, `memory`, `attachment`. **Memory is just a Markdown
document** (a `kind: memory` note in a chosen folder) — there are **no dedicated
`/memories` routes**; read/search/edit it through the ordinary document API.

### Folders

| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/api/v1/spaces/{space}/folders` | list / create `{path}` |
| POST | `/api/v1/spaces/{space}/folders/rename` | rename a subtree `{from, to}` |
| POST | `/api/v1/spaces/{space}/folders/trash` | trash a subtree `{path}` |
| GET | `/api/v1/spaces/{space}/folders/trash` | list trashed folders |
| POST | `/api/v1/spaces/{space}/folders/restore` | restore a trashed folder |

### Attachments

| Method | Path | Scope | Purpose |
|---|---|---|---|
| GET | `/api/v1/spaces/{space}/attachments` | read | list attachments |
| GET | `/api/v1/spaces/{space}/attachments/{path}` | read | download bytes |
| PUT | `/api/v1/spaces/{space}/attachments/{path}` | write | upload (`application/octet-stream` body) |

---

## 8. Search, tags, graph, memos

| Method | Path | Scope | Purpose |
|---|---|---|---|
| GET | `/api/v1/spaces/{space}/search` | `search:read` | prefix full-text (FTS5/BM25) |
| GET | `/api/v1/spaces/{space}/tags` | read | tag counts for the vault |
| GET | `/api/v1/spaces/{space}/graph` | read | legacy link graph (`?limit`) |
| GET | `/api/v1/spaces/{space}/graph/analysis` | `graph:read` | scoped analysis (see below) |
| GET | `/api/v1/spaces/{space}/graph2` | `graph:read` | complete lightweight graph |
| GET | `/api/v1/spaces/{space}/memos` | read | paginate memo-tagged documents |

**Search** params: `q` (≤400), `limit` (1–100, default 25), `offset`
(0–1,000,000), `tags` (comma-separated, intersected), `folder` (relative prefix
with exact boundaries), `tagMode` (`subtree` default \| `exact`), `path`.
Returns `{hits, limit, offset, hasMore}`. Words match token **prefixes**, ranked
BM25; punctuation-only `q` returns nothing.

**Graph analysis** params: `scope` (`vault` default \| `folder` \| `note` \|
`selection`), `scopeId[]` (note/selection), `scopePath` (folder), `query`
(title/path/tag search before the cap), `direction` (`both` default \| `in` \|
`out`), `depth` (0–3; default 0 for vault/folder, 1 for note/selection), `limit`
(≤1000). Responses flag `truncated` when the 1000-node cap is hit.

**Graph2** returns *all* indexed note nodes + unresolved nodes and deduplicated
explicit links — no community analysis, no node sampling — for continuous
canvas navigation.

**Memos** params: `folder` (omitted = all, empty = vault root, else descendants),
`tag`, `q` (literal, ≤400), `from`/`before` (creation timestamps), `cursor`
(opaque, bound to vault + filters), `limit` (default 20, max 50). Returns memos
with revision/tags/creation time, matched total, available tag counts, and
`nextCursor`.

---

## 9. Events

`GET /api/v1/spaces/{space}/events` — a `text/event-stream` (SSE) of workspace
live updates for that vault.

---

## 10. Schemas (selected)

```jsonc
// DocMeta
{ "id": "doc_…", "path": "Daily/2026-10-12.md", "kind": "note",
  "currentRevision": "rev_…", "updatedAt": "2026-10-12T09:00:00Z" }

// SaveResult
{ "revision": "rev_…", "committedAt": "2026-10-12T09:00:00Z" }

// InstanceSettings
{ "authMode": "local", "needsSetup": false, "signupEnabled": false,
  "version": "…", "features": { "…": true },
  "runtime": { "kind": "web",
    "auth": { "passwordLogin": true, "oauth": false, "logout": true },
    "storage": { "defaultProvider": "local", "providers": ["local","s3"],
                 "folderPicker": "none" } } }

// VaultStorage (PUT …/storage)
{ "provider": "s3", "bucket": "…", "region": "…", "endpoint": "…",
  "prefix": "…", "pathStyle": false, "auth": "keys|session|oidc",
  "accessKey": "…(writeOnly)", "secretKey": "…(writeOnly)",
  "sessionToken": "…(writeOnly)", "stsEndpoint": "…", "roleArn": "…" }
```

`Role` ∈ `owner`, `editor`, `viewer`. `Credentials` = `{email, password(≥8),
displayName?}`.

---

## 11. Worked examples

**Log in, create a note, update it safely (cookie session):**

```bash
BASE=http://127.0.0.1:3000/api/v1
curl -s -c jar.txt -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"me@example.com","password":"…"}'

# create (idempotent)
curl -s -b jar.txt -X POST "$BASE/spaces/$SPACE/documents" \
  -H 'Content-Type: application/json' -H 'X-Netsidian-Requested: 1' \
  -H 'Idempotency-Key: create-2026-10-12-01' \
  -d '{"path":"Daily/2026-10-12.md","content":"# Today\n"}'

# read → capture ETag/revision, then update with If-Match
REV=$(curl -s -b jar.txt -D- "$BASE/spaces/$SPACE/documents/$DOC" -o /dev/null \
      | sed -n 's/^[Ee][Tt][Aa][Gg]: *//p' | tr -d '\r')
curl -s -b jar.txt -X PUT "$BASE/spaces/$SPACE/documents/$DOC/content" \
  -H 'Content-Type: application/json' -H 'X-Netsidian-Requested: 1' \
  -H "If-Match: $REV" \
  -d '{"markdown":"# Today\n\nwritten.\n"}'
```

---

**See also:** [authentication.md](authentication.md) · [data-model.md](data-model.md)
· [agents.md](agents.md) · live spec `GET /api/v1/openapi.json`.

Password change is available in local and mixed modes when the account has a
local password. `/me.canChangePassword` advertises eligibility to the UI.
Already executing authenticated requests are not cancelled by credential revocation.

Second-factor security changes require fresh authentication. Pending enrollment
expires after ten minutes and is bound to the password verified at setup.
Confirmation, disable and operator reset revoke all of the affected account's
sessions and personal tokens. Save recovery codes before confirmation and sign
in again afterward. Regeneration and password changes accept an unused recovery
code or authenticator code with the current password. Operator reset requires the
operator's current local password and their own second factor when enabled;
the audit identifies the operator and target separately. This recovery path needs
another available local operator if the affected operator lost both device and
recovery codes; no offline administrative bypass is provided.

Each challenge permits five attempts over five minutes. An account-wide limit of
ten factor verifications per five minutes also applies across new challenges,
client addresses and service restarts. Accepted factors, challenge completion,
session issuance/security changes and success audit records commit together;
failed effects cannot spend a recovery code. Factor attempt budgets are counted
separately even when verification or the subsequent operation fails.

### Attachment file operations

`POST /api/v1/spaces/{space}/attachments/move` accepts `{from,to}` and refuses
an occupied destination or file-type change. `DELETE /api/v1/spaces/{space}/
attachments/{path}` moves a file to recoverable app trash and returns
`{trashId,path,kind:"attachment"}`. Attached folders do not support this app-trash
operation. The existing folder-trash catalog includes these entries; use
`POST /api/v1/spaces/{space}/folders/restore` with `{trashId}` to restore a file.
Conflicts retain the original in trash and never overwrite the live replacement.
Vault permissions, attachment-write scopes and cookie CSRF checks apply.


### Folder-backed Kanban

Boards are folders identified by a versioned `board.md`. Status/membership derive
from task locations. See [Kanban](kanban.md).

| Method | Path under `/api/v1/spaces/{space}` | Behavior |
| --- | --- | --- |
| GET / POST | `/boards` | List summaries / create `{path,title,template?,columns?}`; creation supports Idempotency-Key |
| GET | `/boards/board?path=...` | Resolved columns/cards, supporting documents, configuration revision/ETag and warnings |
| PUT | `/boards/config?path=...` | `{columns,title,createDefaults?}` with board If-Match; populated column removal/folder changes refused |
| POST | `/boards/tasks?path=...` | `{columnId?,title,content?}`; supports Idempotency-Key; uses creation defaults when omitted |
| POST | `/boards/move?path=...` | `{docId,columnId,fromPath}` with task If-Match; preserves identity and repairs supported links; returns any repair warnings |
| POST | `/boards/reorder?path=...` | `{columnId,orderedIds}` with board If-Match; unlisted actual cards remain visible |

Paths are URL-encoded query values. Read operations require document read access;
mutations require normal document creation/edit permissions and corresponding
PAT scopes. Destination collisions refuse replacement. Board configuration files
cannot become tasks, and each task belongs to one owning board.
