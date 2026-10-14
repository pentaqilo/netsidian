# Roadmap baseline decisions

Status: baseline decisions for Netsidian 0.1.0, reviewed 2026-10-14.
This document records what the project commits to, what it excludes, and the
acceptance criteria each committed feature must meet. It is the reference the
implementation increments are measured against.

## Corrected feature matrix

Claims from the comparative gap analyses were revalidated against the source.
The corrected position:

| Claim | Reality at baseline | Committed position |
|---|---|---|
| Daily notes absent | Present: `frontend/src/pages/Workspace.tsx` creates/opens `Daily/YYYY-MM-DD.md` | Extend with configurable folder, date format, and templates (Phase B) |
| Calendar absent | Present: `frontend/src/pages/MemosPage.tsx` has a memo calendar | Reuse its date/navigation logic for daily-note navigation |
| Obsidian comments absent | Partially present: `%%inline%%` handled in `frontend/src/lib/markdown.ts` | Multiline `%%` comments with renderer/index parity (Phase C) |
| Agent memory needs formalization | Present: `docs/agents.md` documents ordinary Markdown memory | Improve specific integration workflows only where evidence shows a gap |
| Memory proposals are an advantage | Retired feature (see `docs/agents.md`) | Corrected: not revived implicitly; any revival needs its own decision |
| Excalidraw revisions inherit automatically | Not true: `internal/service/textfiles.go` serves selected formats with ETags and no document revisions | Explicit drawing storage/permissions/history only if demand justifies viewers |
| State-directory backup covers all content | Not true: attached folders and S3 content live outside the state directory | Storage-aware backup inventory and restore verification (Phase A, implemented here) |

Convergence counts in the input analyses mixed single-source requests with
shared gaps; they are not used as demand evidence.

## Supported Markdown profile

The backend indexer currently extracts YAML frontmatter, ATX headings,
wikilinks and embeds, relative Markdown links, inline/frontmatter tags, and task
markers. It ignores fenced and indented code and inline code spans. Rendering
uses the frontend Markdown pipeline, which also supports callouts, diagrams,
footnotes, and inline `%%comments%%`. Renderer/index parity is a Phase C acceptance
gate; this increment does not claim that all interpretations already agree.

Source editing stores ordinary Markdown. Unsupported syntax must remain
preserved; the visual editor retains its existing lossless-save eligibility
gate. CRLF behavior, multiline comments, block references, and bounded
transclusion remain planned integrity work. `.obsidian` settings interpretation
and a byte-preserving properties editor are separate follow-ups.

## Committed scope and acceptance criteria

### Phase A — account and recovery (in progress)

Password change (committed):

- Current-password verification; local-account eligibility only (OIDC-only
  accounts are rejected with an explicit error, not silently accepted).
- Human-session-only: API-key principals cannot change passwords.
- Atomic security state: hash update, session revocation, PAT revocation, and
  the audit event commit in one SQLite transaction; a failed operation leaves
  no partial state.
- Documented revocation policy: changing a password revokes all sessions and
  all personal API keys of that account. Rationale: a password change is the
  standard response to suspected compromise; partial revocation invites
  silent stale credentials. (Recorded decision; TOTP work in Phase A adds
  factor-aware variants in this increment.)
- Audit: `password.change` event with actor, outcome, and counts; visible in
  the admin audit feed.

Second factor — TOTP & recovery (implemented; workspace changes uncommitted):

- Optional, confirmed enrollment for local password accounts; a pending or
  abandoned enrollment never enforces the factor. Secrets are sealed with the
  instance key (`totp:<userID>`) and never returned after enrollment.
- Login with a confirmed factor yields a short-lived, attempt-limited challenge
  (5 minutes, 5 attempts) and **no** session; only the TOTP/recovery challenge
  endpoints issue the session, and a pending challenge authorizes no other API
  operation.
- Atomic replay prevention: each accepted TOTP step is recorded, and recovery
  codes are hashed. Factor consumption, the authorized effect, and the audit
  commit together, so concurrent attempts cannot reuse a factor and failed
  operations cannot spend recovery codes.
- Disable and recovery-code regeneration require the current password **and** a
  valid authenticator or recovery code. `/me` reports `totpEnabled`, `totpPending`, and
  `recoveryCodesRemaining`.
- Operator recovery for lost device + lost codes: `POST /admin/users/{id}/totp-reset`
  (Administration → Reset 2FA), audited as `totp.reset`, clears the factor so
  the member can sign in with their password and re-enroll. Fresh operator
  password and their own factor (when enrolled) are required; target sessions
  and personal tokens are revoked. A sole operator with lost device and lost
  codes needs another available operator; no offline bypass is implemented.
- Restore verification reopens every sealed second-factor secret with the
  restored instance key, so an enrolled factor survives a backup round trip.

Quiesced backup/restore (committed):

- Backup covers: SQLite state (consistent snapshot), secrets, journal,
  revision history, managed local content, attached-folder vaults, and remote
  (S3) content — the full storage-aware inventory, not just the state
  directory.
- A manifest records instance metadata, per-entry SHA-256 checksums, and the
  vault/storage inventory so restore can verify rather than assume.
- Restore targets a new, nonexistent instance directory; it refuses to overwrite an
  existing instance.
- Restore verification proves: accounts/sessions/keys present, sealed storage
  secrets decrypt with the restored instance key, every document revision and
  attachment resolves to content with matching checksums, history files exist,
  and metadata/search rows cover current document revisions. Resolved graph targets
  are checked for missing live documents; full syntax reindex parity remains Phase C work.
- Attached-folder vaults restore into the instance and their bindings are
  rewritten to the restored location; remote-backed vaults restore their bytes
  as managed local content with bindings rewritten to local storage (re-point
  to the provider afterwards). Sealed provider profiles are not silently
  reactivated.
- External writers (editors syncing attached folders) must be quiesced during
  backup; this tooling does not promise online consistency. Online application
  backup is separate, uncommitted work.

### Committed later, with decisions recorded

- ZIP import (#1): plaintext vault ZIP into a new managed space. Excludes
  merge/overwrite and encrypted archives. Needs path validation, symlink
  rejection, name-collision handling, decompressed-size and per-file bounds,
  staged progress with cancellation and restart recovery, and an
  imported/skipped/failed report. Recovery restarts a staged import safely;
  whole-archive transactional atomicity is not promised. Proposed initial bounds:
  20,000 entries, 1 GiB decompressed total, 128 MiB per file. Revalidate these
  against representative vaults when implementing Phase B. Failed imports keep
  a staged report; restarting creates a fresh destination and removes only its
  own abandoned staging after explicit cancellation.
- External reconciliation (before Git import/watchers): content-hash compare,
  preserve prior revision bytes, create/update/delete detection, defined
  rename behavior, conflict reporting against API/browser writes, index
  updates, interruption recovery. First increment treats unproven renames as
  delete/create; deleted notes keep tombstones/history. A rescan aborts a document
  update when its expected revision changes during ingestion and reports a
  conflict, preserving both byte versions. Explicit rescan first; watchers only after
  the operation is safe and idempotent. Reconciliation is not a security write
  guard.
- Sharing (#13): distinct share-token model (not PAT principals), explicit
  single-document sharing first showing current content, attachments authorized
  only through the same capability and bounded to the shared document's references.
  Linked notes/transclusions stay private unless independently shared; safe rendering and cache
  invalidation on revocation from the first public route. Public publishing
  (#16) stays demand-gated and separate.
- Comment/callout fidelity and blocks/transclusion (#6, #9): committed for
  Phase C with renderer/index parity fixtures.
- Daily-note configuration/templates (#8), usage accounting with soft limits
  (#5, #15), desktop CLI/folder opening (U1): committed as extensions of
  existing functionality.

### Deferred / demand-gated (unchanged from roadmap)

TOTP (#10) early in Phase A after password change; autocomplete (#14)
incremental; properties panel and `.obsidian` awareness as separate
follow-ups; slash commands/protected-file prompts as small follow-ups;
PDF tabs and selectable-text Markdown conversion delivered in the file-workflow increment; Excalidraw viewers demand-selected; Git integrations after
reconciliation; AI writing help as a bounded experiment; agent panel (#20),
WebDAV (#21), i18n/RTL (#22) prepared incrementally or gated on concrete use;
account purge/metrics (#23) split and scoped independently; E2E, chat, Office,
EPUB, Bases (#26–#29) uncommitted backlog. Billing, Postgres migration,
editor-mode replacement, and server-side agent CLI execution remain deferred.

## Recovery behavior summary

- Lost password (local account): no self-service email reset exists; an
  account holder must know the current password to change it. No administrator
  password-reset endpoint is introduced here. OIDC-only accounts recover via
  their provider.
- Lost instance: quiesced backup + isolated restore is the supported path;
  restore verification output is the evidence of success.
- Failed password change: password, sessions, tokens, and success audit remain
  unchanged. A successful change revokes sessions and PATs atomically.
- Interrupted restore: a marker blocks application startup; a failed normal run
  removes its newly created target. After process interruption, retain the partial
  target for diagnosis and restart into another new directory.
- Incomplete backup: unavailable attached/remote storage, symlinks, and unresolved
  operations fail the command. Reconcile operations and quiesce all writers first.

## App branding (U2)

The web workspace, shell and authentication screens share the Netsidian N
mark defined in `frontend/src/lib/brand.ts`: two foreground stems joined by
an accent diagonal. Inline marks inherit the application's resolved
`--accent`; their foreground follows the theme. The favicon serializes the
same geometry and effective accent, updates on appearance changes, and
retains a static SVG fallback (`frontend/public/icons/netsidian.svg`).
Native OS/package icons remain a separately verified platform follow-up.
