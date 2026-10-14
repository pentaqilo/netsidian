# Configuration

Everything is environment variables — no config file. Defaults are safe; the app **fails closed** on unsafe or unimplemented combinations.

## Security model first

| Combo | Result |
|---|---|
| `AUTH_MODE=none` + loopback bind | ✅ private single-user instance |
| `AUTH_MODE=none` + `0.0.0.0`/public bind | ❌ **refused at startup** — by design |
| `AUTH_MODE=local` + any bind | ✅ accounts required |
| `DATABASE_DRIVER=postgres` | ❌ unimplemented, refused (SQLite only) |

For an instance reachable by others, use `local`, `external`, or `mixed` authentication. TLS termination belongs in a reverse proxy; set `NETSIDIAN_TRUSTED_PROXY=true` + `NETSIDIAN_PUBLIC_URL` when you do that.

## Reference

| Variable | Default | Description |
|---|---|---|
| `NETSIDIAN_BIND` | `127.0.0.1` | Listen interface |
| `NETSIDIAN_PORT` | `3000` | Listen port |
| `NETSIDIAN_PUBLIC_URL` | — | External URL when behind a proxy |
| `NETSIDIAN_AUTH_MODE` | `none` | `none` \| `local` \| `external` \| `mixed` |
| `NETSIDIAN_SIGNUP_ENABLED` | `false` (Docker/Compose: `true`) | Allow additional local accounts after initial administrator setup; `false` closes registration without affecting sign-in |
| `NETSIDIAN_LOCAL_FOLDER_PICK` | `false` | Single-machine/local deployments only: lets logged-in sessions browse the server's folders and attach one as a new vault root (in-place, nothing copied). Never enable on a shared/public instance — it exposes directory names to every session. |
| `NETSIDIAN_TRUSTED_PROXY` | `false` | Honor `X-Forwarded-Proto` from your TLS proxy |
| `NETSIDIAN_STATE_DIR` | `~/.local/state/netsidian`¹ | Root for all durable state |
| `NETSIDIAN_STORAGE_ROOT` | `<state>/content` | Where vault folders live (keep on the same volume) |
| `NETSIDIAN_STORAGE_MODE` | `managed` | `managed` only; unsupported modes are refused |
| `NETSIDIAN_DATABASE_DRIVER` | `sqlite` | Only `sqlite` is implemented |
| `NETSIDIAN_SQLITE_PATH` | `<state>/database/netsidian.db` | Database file |
| `NETSIDIAN_SQLITE_BUSY_TIMEOUT_MS` | `5000` | Write-lock patience |
| `NETSIDIAN_MAX_NOTE_BYTES` | `2097152` | 2 MiB per note |
| `NETSIDIAN_MAX_ATTACHMENT_BYTES` | `26214400` | 25 MiB per attachment |
| `NETSIDIAN_REVISION_RETENTION_DAYS` | `30` | Age at which old revisions may be pruned |
| `NETSIDIAN_TRASH_RETENTION_DAYS` | `30` | Trash purge horizon |
| `NETSIDIAN_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `NETSIDIAN_INSTANCE` | `server` | Set by the desktop build; leave alone |

¹ Actually `NETSIDIAN_STATE_DIR` > `$XDG_STATE_HOME/netsidian` > `~/.local/state/netsidian`.

## Data locations

```
<state>/
├── content/spaces/<spaceID>/   # YOUR NOTES: plain .md + attachments, portable
├── database/netsidian.db       # SQLite (WAL); back up while app is stopped or via snapshot
├── journal/ops                 # idempotency journal for save retries
├── history/                    # immutable revision bytes
└── secrets/auth_secret         # 32-byte secret: encrypts OAuth flows + seals storage-provider credentials (auto-generated)
```

Use storage-aware [backup/restore](backup.md). A state-directory archive alone
does not cover external managed content roots, attached folders, or S3 vaults.

## Accounts & recovery

- First visit with `auth=local` and no accounts → setup page creates the operator account (signup may then stay off).
- The supplied Docker image and Compose deployment open registration after setup. Set `NETSIDIAN_SIGNUP_ENABLED=false` to close it. Each new account receives a private personal vault and has no administrator rights. Local email addresses are not verified and never grant privileges.
- **Settings → Administration** is available to signed-in administrators. Search accounts, enable/disable access, change local administrator roles, sign accounts out, and inspect the account audit. Disabling an account revokes sessions and API keys and retains its data. Re-enabling does not restore revoked credentials. Administrators still need vault membership to read a vault.
- You cannot disable or demote your own account or remove the last enabled local administrator. API keys cannot use administration endpoints. SSO roles are managed by claim policy, while a local disable also blocks SSO login.
- There is **no e-mail password reset**. Signed-in users can change their own password (Settings → Account), which revokes all their sessions and API keys. Creating another account does not grant access to the original account’s private vaults. Keep instance backups (`netsidian backup`, see [backup.md](backup.md)) and portable vault exports; restore a known working backup when recovering an inaccessible instance.
- Sessions are persisted in SQLite and carried in the `nsd_session` cookie (`HttpOnly`, `SameSite=Strict`). Local passwords are hashed with **Argon2id**. Rotating `<state>/secrets/auth_secret` invalidates in-progress OAuth flows and re-seals storage-provider credentials; it does not revoke existing sessions. See [authentication.md](authentication.md).
- **Optional two-step verification (TOTP)** needs **no configuration** and is off by default. Local password accounts enroll it from Settings → Account; the shared secret is sealed with the instance key. Defaults: 30-second step, 6 digits, ±1 step tolerance, 10 recovery codes, login challenges valid 5 minutes with at most 5 attempts. An operator can reset a member's factor (Administration → Reset 2FA) if both the device and recovery codes are lost.

## Limits you'll feel

2 MiB notes, 25 MiB attachments, 1000-node cap on graph analysis responses (flagged via `truncated:true`), ~5 s SQLite write timeout. Note, attachment, retention and SQLite timeout limits are configurable. The graph cap is fixed; narrow the folder or search to explore large vaults.

## OAuth / OpenID Connect

Set `NETSIDIAN_AUTH_MODE=external` for provider-only login, or `mixed` for local
accounts plus provider login. Configure:

In `mixed` mode, complete the initial local administrator setup before the first provider sign-in. This preserves a local administrator for recovery.

- `NETSIDIAN_OIDC_ISSUER`: provider issuer URL, discovered using OpenID Connect.
- `NETSIDIAN_OIDC_CLIENT_ID`: registered application client ID.
- `NETSIDIAN_OIDC_CLIENT_SECRET`: client secret for confidential clients; omit for public clients.
- `NETSIDIAN_PUBLIC_URL`: browser-facing origin (HTTPS outside loopback).
- `NETSIDIAN_OIDC_LABEL`: button label, default `Single sign-on`.
- `NETSIDIAN_OIDC_SCOPES`: space-separated scopes, default `openid profile email`. Include a provider-specific scope such as `groups` when needed; `openid` is always requested.
- `NETSIDIAN_OIDC_ALLOWED_CLAIMS`: JSON admission rules, e.g. `{"/groups":["netsidian-users","netsidian-admins"]}`. Set this, or explicitly set `NETSIDIAN_OIDC_ALLOW_ALL_USERS=true`, to enable admission. Missing admission configuration fails startup. Do not set both.
- `NETSIDIAN_OIDC_ADMIN_CLAIMS`: separate administrator rules, e.g. `{"/groups":["netsidian-admins"]}`. Required in `external` mode; in `mixed` mode the first local account can administer the instance instead.

Rule keys are JSON Pointer paths: `/groups`, `/roles`, `/realm_access/roles`, or `/https:~1~1example.com~1roles` for a namespaced claim. Every configured path must match; values at each path are alternatives. Only exact, case-sensitive string values or arrays containing those strings match. Missing claims, wrong types, partial matches, and empty rules deny access. Include the administrator group in the admission rule too: an administrator rule does not bypass admission.

For example, a provider that includes groups in its signed ID token can use:

```sh
NETSIDIAN_AUTH_MODE=external
NETSIDIAN_OIDC_SCOPES='openid profile email groups'
NETSIDIAN_OIDC_ALLOWED_CLAIMS='{"/groups":["netsidian-users","netsidian-admins"]}'
NETSIDIAN_OIDC_ADMIN_CLAIMS='{"/groups":["netsidian-admins"]}'
```

Claims must be included in the verified **ID token**. Unsigned frontend claims and access-token payloads are never used for authorization. Provider-side group overage references are denied; configure the provider to include the required claim values. With `mixed` mode, local accounts use the separate local policy; close local registration or use `external` when admission must be exclusively enforced by SSO.

Register the exact redirect URI
`<NETSIDIAN_PUBLIC_URL>/api/v1/auth/oauth/callback` with the provider. Behind a
trusted TLS proxy, also enable `NETSIDIAN_TRUSTED_PROXY=true`. The provider must
support authorization code flow, PKCE S256, OpenID Connect discovery, and signed
ID tokens. OAuth flow cookies expire after ten minutes. Identity tokens are
verified for signature, issuer, audience, expiration, and nonce.

Each admitted issuer/subject pair owns an account and a personal vault. External
administrator rights come only from the administrator claim rules, including for
the first external account. Verified emails must be unique; an existing email produces an
account conflict, never an automatic account link. Unverified email claims are
not stored. Local account passwords cannot sign in when using `external` mode.

External claim evidence and sessions expire at the ID token expiry or one hour,
whichever is sooner. Signed-in users and API keys are checked against the current
claim policy on every request; configuration changes take effect after restart.
A new sign-in refreshes provider roles, and a role change revokes older sessions.
Provider membership changes become effective at the next sign-in or evidence
expiry; Netsidian does not implement continuous IdP notifications or back-channel
logout. API keys belonging to external accounts also require fresh SSO evidence
within that hour. Existing external accounts must sign in once after upgrading.

## S3 content storage

Set `NETSIDIAN_STORAGE_DRIVER=s3` and `NETSIDIAN_S3_BUCKET` to an existing bucket.
New managed vaults store Markdown, attachments, folder markers and trash as S3
objects. Existing `local-main` vaults continue using their local content directory;
choosing S3 does not silently migrate existing notes. Folder-attached vaults stay
on their selected filesystem. Restore the same SQLite database, journal and
history together with the matching bucket contents when restoring backups.

Optional configuration:

- `NETSIDIAN_S3_REGION` (default `us-east-1`).
- `NETSIDIAN_S3_PREFIX` (default `netsidian`): a dedicated prefix for this instance.
- `NETSIDIAN_S3_ENDPOINT`: custom endpoint for an S3-compatible service.
- `NETSIDIAN_S3_PATH_STYLE=true`: path-style addressing, useful with MinIO.

Credentials use the standard AWS SDK credential chain: environment variables
(`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, optional `AWS_SESSION_TOKEN`),
shared credential files, or workload identity. Do not put credentials in browser
settings. Grant bucket listing and object read, write, copy, and delete within
this instance's prefix. The bucket must exist before startup. SQLite, immutable
revision history, and the operation journal still require a persistent state
volume. S3 objects are replaced atomically; folder moves use compensating copies
because S3 has no directory rename operation. One Netsidian writer per state
volume is required.
