# Netsidian adapter

Inspect actual tool schemas before first use.

Use netsidian_connect for actual vault IDs, per-vault grants and policy version.
Use configured vaultId/folder/scopeId. Supply credentials
separately, never in notes or URLs. Respect 401/403 and Retry-After; no fallback.

Use netsidian_memory_recall for compact entries: prefer known key, active status,
scopeId and an 8192-byte budget. Matching is substring, not semantic ranking.
Continue cursors unchanged; restart on conflict. History needs its grant.
IDs survive rename; folder links do not.

Use netsidian_memory_remember with compact text (500 Unicode characters), optional
exact detail (20000), current index revision for edits, and logical idempotency key.
 refs navigate to latest resources; sourceRefs pin
revision evidence, e.g. {"target":"doc_ID","kind":"document","revision":"rev_ID"}.
Use discovered real IDs. Index already-correct wiki decisions rather than rewriting
them. Preserve exact wording in detail/quote. Longer evidence uses an ordinary
verified document plus an index pointer; never truncate silently.

Verified remember detailRevision confirms exact read-back.
Generic writes require verification; receipts confirm persistence, not truth. For corrections use
netsidian_memory_revise; for consolidation inspect netsidian_memory_consolidate
and dry-run first. Partial/unknown outcomes require original operation identity
and unchanged payload; never repeat with a fresh key to bypass recovery.

Ordinary project knowledge uses netsidian_search and document_read/create/update.
Link authoritative wiki/boards. Graph requires its grant. Focused create uses markdown; REST create uses content; REST content update
uses markdown and If-Match. Use document IDs or explicit by-path lookup.

Index limit is 1 MiB: explicitly partition configured scoped folders before growth
exceeds it. No automatic migration. Load detailed schemas/recovery only when needed:
package references or netsidian_memory_policy (schema/examples/recovery); do not preload
all adapters.
