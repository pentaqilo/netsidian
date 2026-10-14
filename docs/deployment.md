# Deployment

From "works on my laptop" to reachable from your phone — safely.

## Reverse proxy + HTTPS

Netsidian speaks plain HTTP; terminate TLS in front of it.

```
internet ──TLS──▶ nginx/caddy ──http──▶ netsidian (127.0.0.1:3000)
```

```nginx
location / {
    proxy_pass http://127.0.0.1:3000;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

Required app env for this shape:

```
NETSIDIAN_BIND=127.0.0.1          # keep the app loopback-only
NETSIDIAN_AUTH_MODE=local         # accounts, always, for anything proxied
NETSIDIAN_TRUSTED_PROXY=true      # trust X-Forwarded-Proto
NETSIDIAN_PUBLIC_URL=https://notes.example.com
```

The app refuses `none` auth on public binds — this is a feature, see [configuration](configuration.md#security-model-first).

## Docker host

- Volume: mount the whole state dir (`/netsidian-state` in the image).
- Update dance: build new tag → `docker rm -f netsidian` → run new tag with the **same volume** → verify `curl -f localhost:3000/healthz`. Never delete the volume to "fix" an upgrade.
- The distroless image has no shell; do all probing from the host.

## Kubernetes / Helm

Chart: [`helm/netsidian/`](../helm/netsidian) — full parameter docs in [`helm/README.md`](../helm/README.md). Tested shape: single-node k3s/k3d, local-path storage, ClusterIP, optional Ingress.

Essentials:
- **replicas = 1** (SQLite on RWO PVC — never RWX two pods at one DB)
- PVC size: start 5Gi; content + revisions grow here
- Bootstrap with `signupEnabled: "true"`, create your account, **upgrade again with `"false"`**
- Registry-less clusters: import the image, keep `pullPolicy: IfNotPresent`
- Rolling update keeps the PVC; verify pod Ready + `/healthz` after each release

## Backups

The state dir holds database + content + journal + history + secrets — but it
is **not** the whole instance: attached-folder vaults and S3-backed vaults keep
their bytes outside it. Use the built-in storage-aware tool (quiesced, verified,
with a manifest inventory):

```bash
# stop the instance, then:
netsidian backup --archive /offbox/nsd-$(date +%F).zip
# restore into an new isolated directory:
netsidian restore --archive /offbox/nsd-2026-10-13.zip --state-dir /srv/nsd-new
```

Details, archive layout and restore verification: [backup.md](backup.md).

```bash
# legacy same-machine snapshot (only complete if you also copy attached
# folders and any S3 bucket contents):
docker stop netsidian
tar czf netsidian-state-$(date +%F).tar.gz -C netsidian-state .
docker start netsidian
```

- Cron it; test a restore quarterly (new directory + archive + same image).
- Application-level insurance: **Settings → Export vault (ZIP)** per vault, any time.
- Restore = new volume from archive → start → login. Upgrades after restore are just newer images.

## Scaling notes

Scale **up** (bigger box) before out (more replicas): SQLite is single-writer by design, which keeps the thing simple and fast for personal/team use. If you genuinely need many concurrent writers, that future is tracked in the roadmap (postgres adapter is scaffolded but unimplemented — the app refuses it).

## Monitoring

- `GET /healthz` for liveness
- `NETSIDIAN_LOG_LEVEL=info` logs startup contract (bind/auth/mode) and errors; set `debug` while diagnosing, not permanently
