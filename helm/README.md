# Netsidian Helm chart

The chart deploys the same server binary and `NETSIDIAN_*` configuration contract as Docker and Compose. It uses one StatefulSet replica and a persistent volume for SQLite, journals, revision history and encrypted credentials. S3 moves current documents and attachments to a bucket; it does not remove the need for this volume or make SQLite safe for multiple replicas.

```bash
helm install netsidian ./helm/netsidian -n netsidian --create-namespace
```

## Release packages

GitHub Actions lints and renders the chart, then adds `netsidian-<version>.tgz` to each [GitHub release](https://github.com/pentaqilo/netsidian/releases). Packaged chart versions and application versions match the release tag; the default image is `ghcr.io/pentaqilo/netsidian:<version>`. Source values retain the local development image.

Download the chart and install it, using your cluster's default StorageClass:

```bash
helm install netsidian ./netsidian-0.2.0.tgz \
  --namespace netsidian --create-namespace \
  --set persistence.storageClassName=""
```

Release checksums include the chart archive. The release is published only after chart validation and all other build jobs succeed.

## Runtime options

| Value | Default | Behavior |
|---|---|---|
| `auth.mode` | `local` | Password authentication; also supports `external` or `mixed` OIDC |
| `signupEnabled` | `"false"` | First administrator setup remains available in local/mixed mode |
| `storage.driver` | `local` | Service default storage; `s3` uses configured bucket |
| `env` | `{}` | Additional non-secret application configuration |
| `existingSecret` | `""` | Existing Kubernetes Secret with OIDC/AWS credentials |
| `replicaCount` | `1` | Enforced single SQLite writer |
| `persistence.size` | `5Gi` | Full application state |
| `persistence.storageClassName` | `local-path` | Set empty to use cluster default |
| `image.repository` / `image.tag` | `netsidian` / `local` | Override for your registry |
| `image.pullPolicy` | `IfNotPresent` | For k3d, import the image before installing |

The chart fixes bind address `0.0.0.0` and database `sqlite`. No-auth mode is restricted to loopback and cannot be deployed behind a Kubernetes Service. Privileged server folder browsing is disabled. Native desktop mode is selected by the build, never by an environment variable.

`GET /api/v1/settings` publishes the runtime capability descriptor used by the common UI. `GET /livez` checks process liveness; `/api/v1/health` and `/healthz` check database and storage readiness. A backing-service outage should remove a pod from service rather than repeatedly restart it.

## OIDC and S3 example

Use a values file with non-secret configuration:

```yaml
auth:
  mode: external
storage:
  driver: s3
existingSecret: netsidian-credentials
env:
  NETSIDIAN_PUBLIC_URL: https://notes.example.com
  NETSIDIAN_TRUSTED_PROXY: "true"
  NETSIDIAN_OIDC_ISSUER: https://identity.example.com/dex
  NETSIDIAN_OIDC_CLIENT_ID: netsidian
  NETSIDIAN_OIDC_SCOPES: openid profile email groups offline_access
  NETSIDIAN_OIDC_ALLOWED_CLAIMS: '{"/groups":["users","admins"]}'
  NETSIDIAN_OIDC_ADMIN_CLAIMS: '{"/groups":["admins"]}'
  NETSIDIAN_S3_BUCKET: netsidian-vaults
  NETSIDIAN_S3_REGION: eu-central-1
```

The existing Secret supplies `NETSIDIAN_OIDC_CLIENT_SECRET` and, when using static AWS credentials, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and optional `AWS_SESSION_TOKEN`. Workload IAM credentials are also supported by the AWS SDK credential chain. Restrict `NETSIDIAN_TRUSTED_PROXY=true` to deployments whose forwarding headers come from a trusted TLS proxy.

Runtime keys controlled by dedicated values cannot be duplicated in `env`; credential keys must use `existingSecret`. Missing provider configuration fails closed at application startup. See [OIDC and storage configuration](../../deploy/compose/auth-storage.md) for claims, profile mappings and trusted S3/STS endpoints.

## Local k3d testing

```bash
k3d image import netsidian:local -c "<cluster-name>"
helm install netsidian ./helm/netsidian -n netsidian --create-namespace
kubectl -n netsidian port-forward svc/netsidian 3488:80
```

Open http://127.0.0.1:3488 and create the first administrator. No password is baked into the image or chart. Preserve the PVC during upgrades; its deletion can destroy all state depending on the StorageClass reclaim policy.

## Upgrading from chart 0.1

Chart 0.2 adds explicit auth/storage values and closes general signup by default. Review your values file and use `helm upgrade --reset-values -f your-values.yaml ...` to merge the new defaults. Preserve the existing release name, namespace and persistence settings so the same PVC is reused. Old rendered validation snapshots are historical evidence, not the current chart contract.
