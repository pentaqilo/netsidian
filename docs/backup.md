# Backup & restore

Netsidian ships a storage-aware, verified backup/restore tool in the binary
itself. It exists because a plain state-directory tarball is **not** a complete
backup: attached-folder vaults and S3-backed vaults store their bytes *outside*
the state dir, and a restore that ignores them silently produces an instance
with empty vaults.

```bash
netsidian backup  --archive vaults-2026-10-13.zip
netsidian restore --archive vaults-2026-10-13.zip --state-dir /srv/netsidian-restored
```

## Quiesced only

The instance **must not be running** while you back up or restore, and external
writers (editors syncing an attached folder) must also be quiesced. The tool
takes a consistent SQLite snapshot with `VACUUM INTO`, but it does not freeze
your filesystem or bucket mid-run. Online application-level backup is separate
roadmap work; until then, stop the server (or schedule backups while it is
down).

## What the archive contains

A single ZIP (`netsidian-backup/v1`):

| Archive root | Contents |
|---|---|
| `state/database/netsidian.db` | consistent SQLite snapshot (`VACUUM INTO`) |
| `state/secrets/` | instance key (sealed vault profiles are encrypted with it) |
| `state/journal/`, `state/history/` | operation journal and immutable revision history |
| `content/` | managed local vault content (`local-main` bindings) |
| `attached/<spaceID>/` | bytes of every `local-folder:` vault |
| `remote/<spaceID>/` | bytes pulled from S3-backed vaults (`s3-main`, `s3-vault`) |
| `manifest.json` | per-entry SHA-256 + size, vault inventory, state counts, warnings |

Remote capture uses the configured provider credentials. Missing or expired
credentials, unavailable attached folders, unsupported bindings, symlinks, and
special files fail the command rather than produce an incomplete successful
backup. OIDC-federated profiles require a usable token source; the operator CLI
does not currently acquire one, so those backups fail explicitly.

Archives contain secrets and plaintext content. They are created with mode
`0600`, never overwrite an existing archive, and must be outside source storage.
Empty folders and other instance state files are preserved. The tool detects
changed file bytes while writing, but quiescing external writers is still a
requirement. Reconcile interrupted application operations before stopping the
instance; pending operations cause backup to fail.

## Restoring

Restore targets an **isolated, new directory**; it refuses to touch a
path that already exists, including an empty directory or symlink. After extraction the tool:

1. Validates the full inventory before extraction: missing, duplicate, colliding,
   undeclared, symlink, and traversal entries fail. It bounds entries, manifest
   size, and decompression, then verifies SHA-256 and size in a private staging tree.
2. Proves the restored instance key works by decrypting every sealed storage
   profile, OAuth grant, and enrolled second-factor (TOTP) secret. A factor
   whose secret cannot be reopened with the restored key fails the restore, so
   an enrolled factor is never silently unusable afterwards.
3. Rewrites storage bindings to the restored locations:
   - `local-folder:<old path>` → `local-folder:<target>/attached/<spaceID>`
   - `s3-main` / `s3-vault` → `local-main` (bytes restored as managed content;
     the sealed profile is removed so the instance never silently reaches for
     the old bucket — re-point it via vault storage settings if desired)
4. Verifies the result: accounts exist, every live document's current revision
   resolves to bytes with the recorded hash, historical revision files
   exist with recorded hashes (a current revision may live only in note content), and the search/metadata indexes cover every
   live document at its current revision. Stored revision-history paths are
   relocated so the application reads the restored files, not the original host.

Any failed check aborts the restore with a non-zero exit and a specific error —
a Netsidian restore either reports what it verified or refuses to claim
success.

```bash
# full example on a fresh machine
systemctl stop netsidian
netsidian backup --archive /offbox/nsd-2026-10-13.zip
netsidian restore --archive /offbox/nsd-2026-10-13.zip --state-dir /srv/nsd-new
systemctl start netsidian   # pointed at /srv/nsd-new
```

Backup flags `--state-dir` / `--storage-root` override the source layout;
otherwise defaults come from the instance environment. Retain deployment
configuration and external credentials separately; restore does not reconstruct
OIDC settings or deployment environment. Start the restored instance with
`NETSIDIAN_STATE_DIR` pointing at the new directory, default local managed
storage, and `NETSIDIAN_LOCAL_FOLDER_PICK=true` for restored attached vaults.
Restore always uses `<state-dir>/content`; a different `--storage-root` is rejected.

The default restore bounds are 1 TiB total, 64 GiB per file, one million entries,
and a 64 MiB manifest. Failed normal restores remove only their newly created
target. Process interruption leaves `.restore-in-progress`, which blocks startup;
restart into another new directory. Existing target directories are never cleaned
or overwritten. Verification checks hashes and database relationships; it does
not authenticate an archive from an untrusted sender or fully reparse indexes.

## Relationship to other mechanisms

- **Settings → Export vault (ZIP)** stays the per-vault portable format for
  moving content between instances and for users. It is not an instance backup.
- A raw state-dir tarball still works for a same-machine disaster if you also
  copy attached folders and the bucket — the manifest in `netsidian backup`
  is exactly the inventory that tells you whether you did.
- Test restores quarterly. The verification report is the test.
