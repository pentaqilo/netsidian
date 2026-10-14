---
title: Integrations & OAuth
created: 2026-10-12
updated: 2026-10-12
type: entity
tags: [auth, identity, config]
sources: [raw/notes/code-scan-2026-10-12.md]
confidence: high
---

# Integrations & OAuth

Single sign-on via OpenID Connect and the external-identity model that backs
the `external` and `mixed` auth modes ([[auth-modes]]).

## Login flow (`httpapi/oauth.go`)
`GET /auth/oauth/providers` lists configured providers; `/auth/oauth/start` → `/auth/oauth/callback` runs the OIDC dance. State is a sealed AES-GCM cookie `{state, nonce, PKCE-verifier, expiry, next}` (10-minute life); PKCE is `S256`; the `id_token` is verified with go-oidc. Claim names are configurable (`OIDCEmailClaim`/`OIDCNameClaim`/`OIDCEmailVerifiedClaim`, defaults `/email`, `/name`, `/email_verified`) ^[raw/notes/code-scan-2026-10-12.md].

## External identity (`service/external_identity.go`)
- Identity is keyed by **(issuer, subject)** — an email claim is **never** trusted to take over a local account; an email collision returns 409
- A new external user is provisioned their own personal vault ([[vaults-and-spaces]])
- Operator role is granted from `OIDCAdminClaims`; access is allowed by an `OIDCAllowedClaims` match **or** `OIDCAllowAllUsers`
- A later rejected login **withdraws** existing access and revokes sessions; `CheckUserAccess` re-validates stored, verified claim evidence on **every** request, so a restart cannot preserve a stale grant (stored claim lifetime ≤ 1h)
- `mixed` mode requires a local administrator to be set up first

## Keys & integrations ([[http-api]])
`GET/POST/DELETE /me/keys` mints scoped API keys (PATs) with optional per-vault bindings and a read-only flag; `GET /me/integrations` lists connected external identities. `GET /agent/whoami` reports the active key's scopes and bindings ([[agent-interface]]).

## Related
[[auth-modes]] · [[roles-sharing-admin]] · [[config-reference]] · [[http-api]]
