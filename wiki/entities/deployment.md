---
title: Deployment & Environments
created: 2026-09-29
updated: 2026-10-12
type: entity
tags: [deployment, testing, config]
sources: [raw/notes/live-probe-2026-09-29.md, raw/notes/repo-structure-2026-09-29.md]
confidence: high
---

# Deployment & Environments

One image, validated against two target shapes: a **local container** and a
**Kubernetes cluster**. Environment-specific host names, addresses, cluster
names, and fixture credentials are deliberately kept out of the wiki — they
live in the deployment configs, not here.

## Image
- `src/Dockerfile` multi-stage: node build of `src/frontend` → Go build with `-tags server` (SPA embedded). Build context must be `src/`
- Tag convention: `netsidian:<series>-YYYYMMDD-rN` (a build series + date + revision)
- A benign Hadolint warning about the `NETSIDIAN_AUTH_MODE` ENV is pre-existing

## Local container target
```
docker run -d --name <local-target> --network host \
  -v <state-volume>:/var/lib/netsidian \
  -e NETSIDIAN_BIND=<host> -e NETSIDIAN_PORT=<port> netsidian:<tag>
```
A seed script (kept outside the repo) provisions a demo account + vaults for
local validation only. Space ids rotate on every fresh recreate + reseed.

## Kubernetes target
```
# import the built image into the cluster, then:
helm upgrade --install netsidian <repo>/src/helm/netsidian \
  -n netsidian -f <values-file> --set image.tag=<tag> --wait
```
- Chart lives at `src/helm/netsidian`; run helm with the chart path **absolute or from repo root** (from `src/` it mis-parses a relative `deploy/...` path as a repo name)
- Chart forces `NETSIDIAN_BIND=0.0.0.0` + `NETSIDIAN_AUTH_MODE=local` [[auth-modes]]
- A `local-path` PVC persists state across upgrades; space ids are long-lived there
- Verify: pod image/Running, PVC Bound, port-forward `svc/netsidian` → `GET /api/v1/health` ^[raw/notes/live-probe-2026-09-29.md]

## Runtime config (portable)
Deployment shape is expressed entirely through `NETSIDIAN_*` env vars — see
[[config-reference]] for the full contract: bind/port, auth mode, state dir,
storage driver, OIDC, trusted-proxy, local-folder-pick. Nothing environment-
specific belongs in the image itself.

## Gotchas
- Local reseed mints new space ids each time; cluster state is long-lived
- Never log or print credential values in scripts/CI output
- The fuller manual runbook lives in `deploy/` (outside the wiki layers)

## Related
[[go-backend]] · [[persistence]] · [[project-lifecycle]] · [[config-reference]]
