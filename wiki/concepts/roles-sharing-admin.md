---
title: Roles, Sharing & Admin
created: 2026-10-12
updated: 2026-10-12
type: concept
tags: [auth, vault, config]
sources: [raw/notes/code-scan-2026-10-12.md]
confidence: high
---

# Roles, Sharing & Admin

Who may do what, how a vault is shared, and the operator surface.

## Roles
Two layers. **Per-space** roles (`viewer`, `editor`, `owner`) gate vault
operations via the `domain.RoleCan` matrix — the same role the agent interface
reports as `allowedOperations` ([[agent-interface]]):

- **viewer** — read, search, export
- **editor** — + create, edit, attachments, trash, restore
- **owner** — + purge, manage members, manage space

**Instance operator** (`IsOperator`) gates the admin surface. External users earn
operator status from verified claims ([[integrations-oauth]]). API keys carry
**scopes** instead of a role; effective access = role ∩ key-scope ∩ key
vault-binding.

## Sharing (`httpapi/sharing.go`)
`ShareWithUser(userId, role)` and `SearchSharingUsers(q)` back the members and
invitation endpoints ([[http-api]]): `POST /spaces/{space}/members`,
`PUT/DELETE .../members/{member}`, `POST/DELETE .../invitations`, and
`POST /invitations/accept`. Sharing never copies content — it grants a role on
the same vault ([[vaults-and-spaces]]) ^[raw/notes/code-scan-2026-10-12.md].

## Admin (`httpapi/admin.go`)
`RequireAdministrator` gates `GET /admin/settings`, `GET /admin/users`,
`PATCH /admin/users/{id}` (an `AdminChange`), and `GET /admin/audit`. Audit
events are `{id, actor, action, resource, outcome, details, createdAt}`. Admin
settings expose auth mode, signup, and OIDC policy — **never** secrets
^[raw/notes/code-scan-2026-10-12.md].

## Capabilities contract (`httpapi/capabilities.go`)
`RuntimeCapabilities{kind(web|desktop), auth{passwordLogin, oauth, logout},
storage{defaultProvider, providers, folderPicker(none|native|server)}}` — a
public, credential-free description of what the running instance supports
^[raw/notes/code-scan-2026-10-12.md].

## Related
[[auth-modes]] · [[integrations-oauth]] · [[vaults-and-spaces]] · [[http-api]]
