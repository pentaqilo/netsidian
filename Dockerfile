# Netsidian — multi-stage container image.
# Image contract: multi-stage non-root build; the whole state dir is a
# VOLUME (DB + journal + secrets persist, not just the .db); config is
# validated at startup (server fails closed, config.go L68-70).
# NETSIDIAN_DATABASE_DRIVER=sqlite is the only implemented driver (config.go
# L68-70), so no PostgreSQL profile ships here.
#
# syntax: build from the Go module root (this directory):
#   docker build -t netsidian:dev .
#
# Run:
#   Zero-config (anonymous writable volume at /netsidian-state, pre-created
#   in-image owned by nonroot 65532 — bare `docker run` needs nothing else):
#     docker run -p 3000:3000 netsidian:local
#   Persistent named volume:
#     docker volume create netsidian-state && \
#     docker run -p 3000:3000 -v netsidian-state:/netsidian-state netsidian:local
#   Bind-mount fallback for existing data (host dir must be writable by 65532):
#     docker run -p 3000:3000 -v "$PWD/nsd-state:/netsidian-state" \
#       --user 65532:65532 netsidian:local

# ---------- Stage 1: frontend ----------
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /app/frontend
# Clean image: node_modules/.bin symlinks work, so the package.json script is
# safe here ("build": "tsc --noEmit && vite build", frontend/package.json L8).
# (The Taskfile invokes the tools via `node ...` only because the dev mount's
# symlinks are broken.)
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build
# -> /app/frontend/dist  (vite outDir "dist", frontend/vite.config.ts L7)

# ---------- Stage 2: build the Go binary ----------
# go.mod declares `go 1.27.1` (go.mod L3). golang:1.27-alpine is used; if the
# registry mirror lacks the 1.27 tag at build time, substitute the highest
# available golang alpine tag >= 1.27.1 (an older toolchain fails with
# "go.mod requires go >= 1.27.1").
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Embed path: `//go:embed all:frontend/dist` (main.go L13) is relative to the
# module root, so the stage-1 output must land at /app/frontend/dist — exactly
# what this COPY produces.
COPY --from=web /app/frontend/dist /app/frontend/dist
# CGO disabled + modernc.org/sqlite (pure Go, go.mod L10) => static binary.
# The `server` build tag selects the server-mode main (server_mode.go L1).
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags server -trimpath \
    -ldflags="-s -w" -o /out/netsidian .
# Pre-create the state dir with nonroot ownership. distroless/static has no
# shell, so this mkdir/chown must happen HERE (alpine has a shell) and is
# copied into the final stage with --chown. 65532:65532 is the uid/gid of
# distroless "nonroot". Without this, a bare `docker run` dies at startup
# with "mkdir /netsidian-state/secrets: permission denied" because the
# default anonymous volume / root-owned image path blocks the nonroot server
# from creating its secrets dir (config.go L78, loadOrCreateSecret L95-127).
RUN mkdir -p /netsidian-state && chown 65532:65532 /netsidian-state

# ---------- Stage 3: runtime ----------
# distroless/static:nonroot pairs with the static binary; the server needs no
# shell. Non-root: distroless runs as UID/GID 65532.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/netsidian /usr/local/bin/netsidian
# State dir with nonroot ownership (created in the build stage above). Docker
# initializes an anonymous volume from this image path's contents+ownership,
# and a bind mount of an empty dir inherits this 65532:65532 ownership — so a
# bare `docker run -p 3000:3000` can create database/, secrets/, journal/.
# No RUN here: distroless/static has no shell.
COPY --from=build --chown=65532:65532 /netsidian-state /netsidian-state
# Entire state dir is persistent: database/, journal/, history/, content/,
# secrets/ all live under it (config.go L75-77, L89-90, L95-127). Mount the
# WHOLE dir, not just the .db file. Bind a host dir with
# ownership 65532:65532 (distroless nonroot uid/gid) so the app can create
# state/secrets.
VOLUME ["/netsidian-state"]
# Default port 3000 = NETSIDIAN_PORT default (config.go L55).
EXPOSE 3000
# Runtime defaults (all read by internal/config/config.go Load()):
#   NETSIDIAN_PORT=3000       (L55)
#   NETSIDIAN_STATE_DIR=/netsidian-state — overrides the XDG/home default
#       (L42, defaultStateDir L129-138). SQLite path derives from it:
#       $NETSIDIAN_STATE_DIR/database/netsidian.db (L77). Auth secret is
#       auto-generated in $NETSIDIAN_STATE_DIR/secrets/auth_secret (L78,
#       loadOrCreateSecret L95-127) — there is intentionally NO secret env var.
#   NETSIDIAN_BIND=0.0.0.0 + NETSIDIAN_AUTH_MODE=local is the only safe,
#       reachable container default: `docker run -p 3000:3000` works and the
#       first visit runs setup (one operator). auth=none binds loopback only
#       and is meant for host runs or a proxy sidecar, not port mappings.
#       Loopback-behind-TLS-proxy setups set NETSIDIAN_PUBLIC_URL and, when
#       the proxy is narrow, NETSIDIAN_TRUSTED_PROXY=true (secure cookies +
#       correct Origin checks).
ENV NETSIDIAN_BIND=0.0.0.0 \
    NETSIDIAN_PORT=3000 \
    NETSIDIAN_AUTH_MODE=local \
    NETSIDIAN_SIGNUP_ENABLED=false \
    NETSIDIAN_STATE_DIR=/netsidian-state
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/netsidian"]
