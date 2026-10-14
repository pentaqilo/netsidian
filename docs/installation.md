# Installation

Pick one path. All of them keep your data in plain files you own.

## **1. Docker (recommended for a personal server)**

**Prerequisites: Docker 24+.**

```bash
git clone https://github.com/pentaqilo/netsidian.git && cd netsidian
docker build -t netsidian:local .
docker run -d --name netsidian \
  -p 127.0.0.1:3000:3000 \
  -v netsidian-state:/netsidian-state \
  -e NETSIDIAN_LOG_LEVEL=info \
  netsidian:local
```

* Image defaults: bind `0.0.0.0`, auth `local`, state dir `/netsidian-state`.
  - First visit to <http://127.0.0.1:3000> → **setup page**: create the operator account.
* Health check: `curl -f http://127.0.0.1:3000/healthz`.
* Exposing beyond localhost: put TLS in front first — see [deployment](deployment.md).

Upgrade = rebuild with a new tag, `docker stop && docker rm`, run again with the **same volume**. Your data survives; the database migrates itself at startup.

## 2. Docker Compose

`compose.yaml` as shown in the [README](../README.md#docker-compose), then:

```bash
docker compose up -d --build
```

No shell exists in the final (distroless) image — probe health from the host, not with `docker exec`.

## 3. Kubernetes (single-node k3s/k3d tested)

The chart lives in [`helm/netsidian/`](../helm/netsidian). See [`helm/README.md`](../helm/README.md) for full values documentation. Minimal single-node install:

```bash
helm install netsidian ./helm/netsidian \
  -n netsidian --create-namespace \
  --set image.repository=netsidian --set image.tag=local \
  --set image.pullPolicy=IfNotPresent   # registry-less k3d: import first
```

Key guarantees baked into the chart:

* `NETSIDIAN_BIND=0.0.0.0` + `NETSIDIAN_AUTH_MODE=local` (non-overridable; the app would refuse unsafe combos anyway)
* **One replica** — SQLite must not be shared; one RWO PVC holds everything
* `signupEnabled: "true"` only to bootstrap the first account; **set it to `"false"` afterwards**

## 4. Desktop app (Linux, macOS, Windows)

- **Releases**: download the matching `netsidian-<version>-<platform>-<arch>` archive. Linux runs `./netsidian`; Windows runs `netsidian.exe`; macOS includes `Netsidian.app`. Data lives in your user state directory.

* **From source**: use `./build-desktop-linux`, `./build-desktop-macos`, or `.\build-desktop-windows.ps1`. See [desktop build scripts](development.md#desktop-build-scripts) for prerequisites and options. The scripts build the frontend first and package the native application in `dist/`.

## 5. From source (server binary)

Prerequisites: Go ≥ 1.27.1, Node 20+ (or just Docker for the frontend step).

```bash
cd frontend && npm ci && npm run build && cd ..   # emits frontend/dist (embedded)
CGO_ENABLED=0 go build -tags server -o bin/netsidian .
./bin/netsidian        # http://127.0.0.1:3000, single-user auth=none
```

Or with [go-task](https://taskfile.dev): `task default` then `task run`.

## Where your data lands

| Location | Contents |
| --- | --- |
| `<state>/content/spaces/<id>/` | Your Markdown files + attachments — the vaults, as real folders |
| `<state>/database/netsidian.db` | Metadata, revisions, accounts, link index, sessions |
| `<state>/journal/`, `<state>/history/` | Idempotency journal, immutable revision bytes |
| `<state>/secrets/auth_secret` | OAuth-flow + storage-provider secret (rotating it does **not** log sessions out) |

State root: `NETSIDIAN_STATE_DIR` > `$XDG_STATE_HOME/netsidian` > `~/.local/state/netsidian` (Windows: `%USERPROFILE%\.local\state\netsidian`). Container: `/netsidian-state`. Full layout: [configuration](configuration.md#data-locations).

## Next

[Getting started →](getting-started.md)
