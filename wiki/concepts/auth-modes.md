---
title: Auth Modes & Sessions
created: 2026-09-29
updated: 2026-10-12
type: concept
tags: [auth, config]
sources: [raw/notes/live-probe-2026-09-29.md, raw/notes/state-dir-resolution.md]
confidence: high
---

# Auth Modes & Sessions

`NETSIDIAN_AUTH_MODE` selects the mode; `config.go` refuses `none` on a
non-loopback bind ([[persistence]]). Modes: `none | local | external | mixed`.

## Modes
- **none** — single-user, auto-session; the desktop default ([[desktop-app]])
- **local** — accounts in SQLite, **Argon2id** password hash; `GET /settings` exposes `authMode`, `needsSetup`, `signupEnabled` ^[raw/notes/live-probe-2026-09-29.md]
- **external** — single sign-on only; local password login disabled; see [[integrations-oauth]]
- **mixed** — a local administrator plus external (OIDC) users coexist

## Session mechanics ([[http-api]])
- Cookie `nsd_session` (`HttpOnly`, `SameSite=Strict`); sessions are **stored in the SQLite `sessions` table**, not signed into the cookie
- `<state>/secrets/auth_secret` encrypts in-progress OAuth flows and seals storage-provider credentials (HMAC); rotating it does **not** log existing sessions out
- Mutations must send `X-Netsidian-Requested: 1`; the Origin must match Host (or `Sec-Fetch-Site` must not be `cross-site`) — a same-origin/CSRF gate. Bearer-key callers are cookie-free and exempt
- `GET /me` → 401 anonymous. Roles gate per-vault operations ([[roles-sharing-admin]])
- External identities are re-authorized on **every** request from stored, verified claim evidence, so a restart cannot preserve a stale grant ([[integrations-oauth]])

## Credentials & keys
- API keys (PATs) are minted per user under `GET/POST/DELETE /me/keys`, carry scopes and optional per-vault bindings, and can be read-only ([[agent-interface]])
- Never log or print credential values in scripts/CI output

## Related
[[deployment]] · [[persistence]] · [[http-api]] · [[integrations-oauth]]
