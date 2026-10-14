# Troubleshooting

## The app won't start

| Log says | Why | Fix |
|---|---|---|
| `no-auth mode must not bind a public interface` | `none` auth + non-loopback bind is refused **on purpose** | Use `NETSIDIAN_AUTH_MODE=local`, or bind `127.0.0.1` behind a proxy |
| unimplemented driver error | `NETSIDIAN_DATABASE_DRIVER=postgres` set | SQLite only — drop the var ([why](configuration.md#security-model-first)) |
| `address already in use` | Port 3000 taken | `NETSIDIAN_PORT=3001`, or stop the squatter |

## Can't log in / first-run oddities

- **Setup page never appears but login demands accounts** — accounts already exist in this state dir; there are no default passwords, ever. Forgot it? See [recovery](configuration.md#accounts--recovery).
- **"Sign-up is disabled"** — by default. Temporarily `NETSIDIAN_SIGNUP_ENABLED=true`, create the account, set it back to `false`.
- **Everyone unexpectedly logged out** — sessions live in the SQLite `sessions` table, so this usually means the **database** changed (a restore, a different state dir, or a wiped volume), not `auth_secret`. Rotating `<state>/secrets/auth_secret` only invalidates in-progress OAuth flows and re-seals storage-provider credentials; it does not sign sessions.
- **Scripting your instance**: cookie sessions + every non-GET needs header `X-Netsidian-Requested: 1`. Missing header ⇒ 403-looking failures that aren't auth bugs.

## Editing

- **Draft not restored after reload** — drafts live in that browser's localStorage; private mode, a different browser/device, or storage cleanup discards them. Committed revisions are unaffected.
- **"This note can't be edited visually"** — the visual editor refuses docs it can't round-trip losslessly (complex HTML, exotic tables…). That's the data-loss guard working; edit in source mode.
- **Conflict prompt on save** — the note changed server-side since your base revision (second window, sync elsewhere). Choose to reopen/merge; nothing silently overwrites.

## Graph & search

- Graph shows `truncated` / fewer nodes than expected — analysis caps at 1000 nodes per response; scope to a folder or lower depth.
- A `[[link]]` isn't linking — check exact filename (case-insensitive match, `.md` implied); links inside code fences/span are intentionally ignored.
- Search misses a just-typed word — the index updates on save; if you saved via the API without a revision, re-save from the UI.

## Build problems

- Go build complains about missing embed files → build the frontend first ([development](development.md#build--run)).
- `go test` fails only on a host with CGO quirks → run `CGO_ENABLED=0 go test -tags server ./...`; CI uses pure-Go SQLite.
- Frontend type errors for `process` → `@types/node` + `"types": ["vite/client","node"]` in `tsconfig.json`; keep both.
- Desktop build fails on your distro → `gcc`/GTK4/WebKitGTK dev headers missing; use the container toolchain (`Dockerfile.desktop-build`) instead.

## Still stuck?

Run with `NETSIDIAN_LOG_LEVEL=debug`, reproduce once, and attach the startup block (bind/auth/mode lines) plus the failing request when [opening an issue](https://github.com/pentaqilo/netsidian/issues). Redact anything private — logs never contain passwords, but notes paths may.
