# Local Markdown adapter

Use only the configured root/scope. Never edit Netsidian-managed files directly;
use its API/journal adapter. Shared folders grant no authorization or replication.

Use index.md JSON bullets, for example:

    - memory: {"key":"preference.address","text":"Compact claim","scopeId":"user-id","status":"active","refs":[]}

Keep text within 500 Unicode characters; link longer exact wording in a detail
Markdown file. Pin source file IDs/hashes; preserve evidence class, known times,
unknown metadata and human content. Read before merging; do not invent sources.

Install scripts/local_memory.py from the downloaded package before writing.
Copied instructions require that helper already installed; do not improvise one.
Pass paths as quoted arguments. Parents must already exist. Example:

    python local_memory.py read --root /configured/memory --path project/index.md
    python local_memory.py write --root /configured/memory --path project/index.md \
      --revision sha256-from-read --input /tmp/revised-index.md

Use revision missing only for a new file. The helper confines root-relative paths,
rejects symlinks/hard links/special files, and uses an interprocess lock plus hash
comparison, same-directory temporary write, fsync, atomic replacement and exact
read-back. All writers must cooperate; an arbitrary editor is not transactional.
It supports POSIX/Linux with flock, dir_fd and O_NOFOLLOW. Network filesystems,
Windows, cross-host locks and hostile root-owner changes are unsupported.

Writes are single-file, not multi-document atomic. A crash may leave a temporary
file; completion-unknown requires inspection, not blind retry. Back up before
schema upgrades. Agent-side dry-run/reflection is available, but durable multi-step
consolidation recovery, pending queues and provider fallback are not enabled.
