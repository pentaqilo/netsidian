# Netsidian adapter

Inspect tool schemas. Use netsidian_connect for vault IDs, grants and policy version.
Use configured vaultId/folder/scopeId. Keep credentials separate. Respect 401/403
and Retry-After; no fallback.

Recall by key, active status and scopeId within 8192 bytes. Matching is by substring.
Continue cursors unchanged; restart on conflict. History needs its grant.
IDs survive rename; folder links do not.

Use netsidian_memory_remember with text (500 Unicode characters), exact detail
(20000), current index revision for edits and an idempotency key. refs navigate;
sourceRefs pin evidence by real ID and revision. Preserve wording in detail/quote;
link longer evidence in a verified document rather than truncating it.

Verified detailRevision confirms exact read-back. Verify generic writes; receipts
confirm persistence, not truth. Correct with netsidian_memory_revise. Dry-run
netsidian_memory_consolidate first. Recover partial/unknown outcomes using the
original operation identity and unchanged payload.

Ordinary project knowledge uses netsidian_search and document_read/create/update.
For human readers, find and read the topic note before creating one. Lead with
the answer under descriptive headings. Merge repeats with their sources; separate
conditions, history and uncertainty. Preserve technical names, quotes and human
edits. Link topics from an overview when useful. Skip unchanged notes. Agent
memory should link the authoritative note, not copy mutable facts. If two copies
are explicitly required, verify both and report partial completion if interrupted.
Link authoritative wiki/boards. Graph requires its grant. Focused create uses markdown; REST create uses content; REST content update
uses markdown and If-Match. Use document IDs or explicit by-path lookup.

Index limit is 1 MiB: explicitly partition configured scoped folders before growth
exceeds it. No automatic migration. Load detailed schemas/recovery only when needed:
package references or netsidian_memory_policy (schema/examples/recovery); do not preload
all adapters.
