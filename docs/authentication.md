# Authentication & access

How Netsidian proves who you are and what you may touch. Environment-variable
setup lives in [configuration.md](configuration.md); this page explains the
model and the runtime mechanics.

## Auth modes (`NETSIDIAN_AUTH_MODE`)

| Mode | Meaning |
|---|---|
| `none` | Single-user, auto-session. The desktop default. **Refused on a non-loopback bind** — by design. |
| `local` | Accounts in SQLite, **Argon2id** password hashes. `GET /settings` reports `authMode`, `needsSetup`, `signupEnabled`. |
| `external` | Single sign-on (OIDC) only; local password login disabled. |
| `mixed` | A local administrator **plus** external (SSO) users. Complete local admin setup before the first provider sign-in. |

For anything reachable beyond loopback, use `local`, `external`, or `mixed` and
terminate TLS in a proxy (`NETSIDIAN_TRUSTED_PROXY=true` +
`NETSIDIAN_PUBLIC_URL`). See [deployment.md](deployment.md).

## Sessions

- Login (`/auth/login`, `/auth/setup`, or OAuth) issues the cookie
  **`nsd_session`** (`HttpOnly`, `SameSite=Strict`).
- Sessions are **stored in SQLite** (a `sessions` table), not signed into the
  cookie.
- `<state>/secrets/auth_secret` is a 32-byte secret used to **encrypt in-progress
  OAuth flows** and to **seal storage-provider credentials (HMAC)**. Rotating it
  invalidates in-flight OAuth flows and re-seals provider credentials — it does
  **not** log existing sessions out. (Older notes claiming "delete it and
  everyone logs out" are outdated.)
- `POST /auth/logout` ends the current session.

## CSRF guard

Every **non-GET** cookie request must send `X-Netsidian-Requested: 1` and be
same-origin (`Origin` matches `Host`, or `Sec-Fetch-Site` is not `cross-site`).
Missing → `403`. Bearer API keys are cookie-free and exempt. This is why
scripting a logged-in instance "fails like a 403" when the header is absent —
it is not an auth bug.

## API keys (personal access tokens)

Mint keys under **Settings → Personal access tokens for agents** or
`POST /me/keys`. A key carries:

- **scopes** (see the list in [api.md](api.md#roles--scopes)),
- optional **per-vault bindings** (`spaceIds`),
- an optional **expiry** (`expiresInDays`),
- a **read-only** posture by default (write and delete are separate grants).

Effective access = **role ∩ key-scope ∩ key vault-binding**, re-checked per
request. Revocation, role changes, and removal take effect on the next request.
Keys **cannot** use administration or member-management endpoints (those need a
human session). The secret is shown **once**.

## Roles (per vault)

`viewer` → `editor` → `owner`, enforced by the `domain.RoleCan` matrix:

- **viewer**: read, search, export
- **editor**: + create, edit, attachments, trash, restore
- **owner**: + purge, manage members, manage space

Sharing grants a role on the **same** vault; it never copies content. See
[data-model.md](data-model.md#vaults-spaces).

## OpenID Connect (SSO)

Authorization-code flow with **PKCE S256**, OpenID Connect discovery, and signed
ID tokens. Flow state is a sealed, 10-minute cookie; the `id_token` is verified
for signature, issuer, audience, expiration, and nonce.

- Identity is keyed by **(issuer, subject)**. An email claim is **never** used to
  take over a local account — a verified-email collision returns `409`, never an
  automatic link. Unverified emails are not stored.
- Admission requires `NETSIDIAN_OIDC_ALLOWED_CLAIMS` (JSON-Pointer claim rules,
  e.g. `{"/groups":["netsidian-users"]}`) **or** an explicit
  `NETSIDIAN_OIDC_ALLOW_ALL_USERS=true` — never both. Administrator rights come
  only from `NETSIDIAN_OIDC_ADMIN_CLAIMS`. Only the verified **ID token** counts;
  access-token payloads and unsigned frontend claims are ignored.
- Each admitted issuer/subject owns an account + a personal vault.
- Claim evidence and sessions expire at the ID-token expiry **or one hour**,
  whichever is sooner. Users **and their API keys** are re-checked against the
  current claim policy on every request; a role change revokes older sessions.
  Netsidian does not implement back-channel logout — membership changes land at
  the next sign-in or evidence expiry.

Register the exact redirect URI `<NETSIDIAN_PUBLIC_URL>/api/v1/auth/oauth/callback`.
Full variable reference and examples: [configuration.md](configuration.md#oauth--openid-connect).

## Accounts & recovery

- First `local` visit with no accounts → setup page creates the operator.
  Signup may stay off; `NETSIDIAN_SIGNUP_ENABLED=true` opens it temporarily.
- Each new local account gets a private personal vault and **no** admin rights.
  Local emails are unverified and never grant privileges.
- **There is no email password reset.** Recover by restoring a known-good state
  backup or a portable vault export. Disabling an account revokes its sessions
  and keys but retains its data; re-enabling does not restore credentials.
- You cannot disable/demote yourself or remove the last enabled local admin.

### Changing your password

Signed-in humans on local password accounts can rotate their own password from
**Settings → Account** or `POST /api/v1/auth/password` with
`{currentPassword, newPassword}` (10–1024 UTF-8 bytes, different from the
current). The rotation is atomic and revokes **every** active session and
personal API key of the account — including the one making the call — so a
stolen cookie or token cannot outlive the change. The browser must sign in
again afterwards. API-key principals cannot call it (403), and provider-managed
(OIDC-only) accounts have no local password to change (409). When the account
has a confirmed second factor, the request must also carry a valid current
`code` (step-up), so a stolen password alone cannot rotate the credential. The
event is recorded in the audit log as `password.change`.

### Two-step verification (TOTP)

Local password accounts may optionally add a time-based one-time code
(RFC 6238, SHA-1, 6 digits, 30-second step, ±1 step tolerance) from an
authenticator app. The feature is **off by default** and needs no extra
configuration.

- **Enrollment is confirmed, never implicit.** `POST /auth/totp/enroll`
  (current password required) returns a Base32 secret, an `otpauth://` URL and
  ten single-use recovery codes **once**. The factor is *pending* and not
  enforced until a valid code is submitted to `POST /auth/totp/confirm`. A
  failed or abandoned enrollment never enables the factor.
- **Secrets are sealed at rest.** The shared secret is encrypted with the
  instance key (id `totp:<userID>`) and stored only in the `totp_enrollments`
  table; it is never returned after enrollment.
- **Login becomes a challenge.** With a confirmed factor, `POST /auth/login`
  verifies the password and returns `{challenge:true, challengeToken, userId,
  displayName}` **without** a session cookie. The challenge is short-lived
  (5 minutes) and allows at most 5 attempts. Only
  `POST /auth/challenge/totp` or `POST /auth/challenge/recovery` (with the
  challenge token and a valid code) issues the session. A pending challenge
  authorizes **no** normal API operation.
- **Replay and single-use are atomic.** Each accepted TOTP step is recorded
  (`totp_used_steps`) so a code cannot be reused within its window; recovery
  codes are stored hashed and consumed in a single atomic update.
- **Recovery codes.** `POST /auth/totp/recovery-codes` (password + current
  code) replaces the set with a fresh batch, shown once. Losing both the device
  and the codes requires operator recovery below.
- **Disabling** requires the current password *and* a valid current code
  (`POST /auth/totp/disable`). `/me` reports `totpEnabled`, `totpPending` and
  `recoveryCodesRemaining`.
- **Operator recovery (lost device + lost codes).** An operator can remove a
  member's second factor from **Administration → Reset 2FA** or
  `POST /api/v1/admin/users/{id}/totp-reset`. This clears the enrollment and
  its recovery codes so the person can sign in with their password and
  re-enroll. It is audited as `totp.reset` and does not create a session.

Audit actions: `totp.enroll`, `totp.confirm`, `totp.disable`, `totp.recovery`,
`totp.reset`, `totp.challenge`.

**See also:** [api.md](api.md) · [configuration.md](configuration.md) ·
[deployment.md](deployment.md) · [troubleshooting.md](troubleshooting.md)

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
