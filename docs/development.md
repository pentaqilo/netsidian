# Development

## Prerequisites

- Go ≥ 1.27.1, Node 22.12+ (the release workflow uses Node 24), Python 3, [go-task](https://taskfile.dev) (optional but handy)
- Linux desktop mode additionally: GTK4 + WebKitGTK-6.0 dev packages, `gcc`, `pkg-config` — or use the `Dockerfile.desktop-build` toolchain image

## Build & run

```bash
task default            # SPA (tsc --noEmit + vite build) → embedded into bin/netsidian
task run                # http://127.0.0.1:3000, auth=none, scratch state dir
```

Raw equivalents (what Taskfile does):

```bash
cd frontend && npm ci && npm run build && cd ..
CGO_ENABLED=0 go build -tags server -o bin/netsidian .
```

The Go binary **embeds `frontend/dist`** — a stale/missing dist means a stale/missing UI. Rebuild both, always, for anything real.

Hot-reload frontend: `cd frontend && npm run dev` with `NETSIDIAN_DEV_API=http://127.0.0.1:3000` in `frontend/.env.local`-style env — Vite proxies to the running Go server.

## Desktop build scripts

Run the script for your platform from any working directory:

```bash
./build-desktop-linux --version 0.1.0
./build-desktop-macos --version 0.1.0 --arch arm64
```

On Windows, run in PowerShell:

```powershell
.\build-desktop-windows.ps1 --version 0.1.0 --arch amd64
```

`./build-desktop` selects the host platform on Linux, macOS and Git Bash. All scripts build the frontend and native executable from the current source in a temporary directory, including uncommitted source changes. They leave `frontend/dist` and `bin` untouched. Python 3, Node.js 22.12+ and npm are required on the host; frontend dependencies are installed with `npm ci` in the temporary tree.

The default version is `0.0.0`, and the architecture defaults to the host. Packages go to `dist/netsidian-<version>-<platform>-<arch>/` with a matching `.tar.gz` (Linux) or `.zip` (Windows/macOS). Use `--output DIRECTORY` for a different destination. Existing packages and archives are refused; choose a new destination or move the previous build first. Each package includes the license and installation instructions.

Linux `--mode auto` uses a host Go/GTK4/WebKitGTK 6.0 toolchain when present and falls back to `Dockerfile.desktop-build`. Use `--mode host` or `--mode container` to select explicitly. Docker must be running for container builds; Node/npm still run on the host. Building a different Linux architecture requires the matching native compiler or Docker emulation.

macOS requires Go and Xcode command line tools (`xcode-select --install`). It produces an ad-hoc signed `Netsidian.app`; Apple notarization is separate. Build Intel and Apple Silicon packages with `--arch amd64` and `--arch arm64` respectively.

Windows requires Go and produces `netsidian.exe` with CGO disabled, matching the release workflow. The target machine needs Microsoft Edge WebView2 Runtime. A Windows package can also be cross-built on Linux/macOS with `python3 scripts/build-desktop.py --platform windows --arch amd64`. Container mode is available only for Linux.

## Test & gates

Every change must pass, in order:

```bash
CGO_ENABLED=0 go vet -tags server ./...
CGO_ENABLED=0 go test -tags server ./... -count=1     # ~1 min
cd frontend && npx tsc --noEmit && npx vitest run && npx vite build
```

- `-tags server` is mandatory (native build compiles a different transport)
- `CGO_ENABLED=0` — pure-Go SQLite driver; keep it that way
- Service suites (`internal/service/*_test.go`) are the real safety net: journeys, safety/idempotency, phase suites. Prefer extending those over new HTTP-level tests

CI-parity tip: run the frontend gates in `node:20` and Go gates in `golang:1.27-alpine` if your host toolchain drifts.

## Repo tour

```
main.go               # entry; go:embed frontend/dist; mode via build tags
internal/
  domain/             ids, models, paths, typed errors — no I/O
  ports/              capability interfaces
  service/            business logic + the big behavioral test suites
  repository/sqlite/  SQLite adapter (migrations/ applied at boot)
  storage/local/      content store: notes+attachments as real files
  markdown/           server-side link parser (mirrored by frontend lib)
  transport/httpapi/  REST /api/v1, sessions, SSE
  bootstrap/          composition root; server_mode.go ↔ native_mode.go (tags)
frontend/             React 19 + Vite 7; pages/, components/, lib/ (+ colocated tests)
helm/netsidian/       chart · wiki/ → contributor knowledge base
```

## Conventions

- Handlers stay thin; rules (validation, caps, idempotency) live in services
- New capabilities = port first, adapter second, wire in `bootstrap` — no cross-package imports into adapters
- Frontend: domain-free pure logic goes in `lib/*.ts` with unit tests (see `lib/accent.ts` as the template)
- UI work follows the compact-controls direction in the phase requirement packs; when in doubt, check the [wiki](../wiki/index.md) first — architecture, API contracts, and design decisions are documented there and kept current

## Architecture wiki

[`wiki/`](../wiki/index.md) is an interlinked, curated map of this codebase (start with `concepts/architecture.md`). If you learn something structural the wiki lacks — update it; that's how it stays useful.
