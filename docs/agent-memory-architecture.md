# Decision: evidence-backed, portable agent memory

Status: implemented and independently validated against reviewed baseline `5603319`.
Policy sources: [portable skill](../skills/agent-memory/SKILL.md) and its adapter,
schema and example references. This document distinguishes the intended contract
from capabilities actually advertised by the running server.

## Authority and reasoning

Ordinary Markdown is canonical knowledge. The existing document service owns
permissions, revision guards, storage-provider behavior and history. MCP and REST
translate requests into the same application operations. SQLite operational
journals record recovery state, not a competing authoritative memory database.
Backups must preserve both Markdown/history and operational recovery state.

The calling agent recalls, acts, selects what to retain and reflects. The server
enforces structure, access, limits and guarded persistence; it cannot establish
that an agent-supplied conversation actually happened. No mandatory LLM call,
embedding database, background extraction or unattended scheduling is introduced.
Reflection is read-only. Durable consolidation is a separate explicit mutation.

## One destination for one scope

Provider, deployment location, audience, scope and stable scope identity are
independent dimensions. A remote Netsidian installation uses the Netsidian
adapter. Personal preferences use private storage; project wiki decisions use the
project destination. Board membership remains task-status authority. Memory
indexes link wiki/board/map resources instead of maintaining parallel work state.
Folders and scope identities do not enforce access. Vault grants remain decisive.
No outage/denial fallback or pending replication queue is enabled.

Settings export the configured workflow; they do not install a host-level agent,
intercept messages or enforce server-wide retention. Existing browser folder
settings stay unadopted until the user selects and adopts the new policy. New
workflows visibly default to selective retention. Settings and exports contain
no credential; a connected agent must load and follow the skill separately.

## Policy distribution

`skills/agent-memory/` is the versioned source package. The core workflow routes
to local, remote and Netsidian adapters. `scripts/generate-memory-policy.py`
creates Go/TypeScript bindings without network access. CI checks reproducibility
and the local helper. The frontend can copy a self-contained selected-adapter
skill or export a ZIP with all adapters, helper and JSON configuration.
Configuration strings are encoded as inert JSON data, not interpolated into
instructions, URLs or shell commands. The server's short guidance and on-demand
policy use the same versioned package, not separately maintained prompts.

## Compatibility and limits

Legacy unversioned JSON bullets remain ordinary Markdown. Provenance absent from
legacy data is unknown. Updating one entry preserves surrounding prose and
unrelated entry bytes. New metadata must preserve unknown stored fields; strict
public inputs are a separate concern. Duplicate keys and unsupported future
schemas must fail mutation rather than silently pick or discard data.

No startup migration is performed. Before upgrading, export/backup documents,
history and recovery journals. Older binaries can drop metadata when writing a
touched entry: do not downgrade and continue editing new-schema records without
a tested restore/export plan. Indexes remain bounded at 1 MiB; explicit scoped
folder partitioning is the growth strategy. Retrieval remains substring filtering
for the compact memory index; ordinary document search has its existing ranking.

## Evidence, correction and recovery

Navigational links follow current resources. Evidence references pin stable IDs
and revisions; rename must not retarget to a same-name substitute. Historical
body access requires history grants. External/interaction locators remain
agent-supplied and unverified; the server never fetches arbitrary URLs or paths.
Recording time is distinct from factual validity; unknown times stay absent.

A corrected stable topic retains an immutable reference to its previous claim
revision. Retraction removes active use while preserving audit history; deletion
and backup retention are distinct operations. Conditions and scope can explain
apparently conflicting observations. Unresolved contradictions stay disputed.

Consolidation is bounded, agent-authored and revision-guarded, with dry-run and
durable step recovery. It is not a multi-document atomic transaction. Partial
outcomes identify exactly what committed, what remains and how to recover.
A retry uses the original operation identity and identical payload; changed input
under that identity fails. Never undo manual changes made after partial work.
Derived outputs remain provisional until their contract is verified; source
changes, trash/retraction or missing evidence require stale/unavailable reporting.
Staleness detection is required; automatic regeneration is not enabled.

## Adapter support

The local adapter ships a tested POSIX/Linux single-file compare-and-swap helper:
root-relative no-follow traversal, interprocess lock, revision comparison under
lock, temporary same-directory write, fsync, replacement and byte verification.
All writers must cooperate. Network filesystem semantics, hostile root-owner
changes, Windows and multi-file recovery are not claimed.

The remote adapter is a guide for discovering a configured provider contract,
not a universal protocol implementation. Conditional writes, stable identity,
authenticated scope, retry safety and verification are prerequisites. Missing
capabilities disable unattended merges/consolidation rather than pretending safe
persistence. The Netsidian adapter uses actual discovered tools and ordinary
vault permissions; denied writes never fall back to another provider.


## Initial shipped operation boundary

A consolidation has at most 16 pinned sources, one topical target and 32 existing
index changes; canonical plan JSON is capped at 96 KiB. Index changes require
an explicit index revision. Claim sources pin that index too. Existing document
targets require a revision; adopting an unowned document appends an explicit
managed block instead of replacing human content. Future marker schemas are
refused. Corrections precede derivation from a changed claim meaning.

The frozen plan and prepared output bytes are durable before the first write.
Restart reconciles actual committed child revisions into checkpoints without
applying new knowledge writes. Client retries recheck authorization and exact
bytes/revisions, including unreconciled external storage edits. A final receipt
failure remains partial/unknown until recovered; matching a status never proves
ownership. Small durable request bindings also protect remember/revise when their
final receipt is lost. Changed payloads under an existing identity fail.

SQLite publishes the verified journal phase and generation event together.
Final validation is serialized against ordinary vault mutations; this does not
make earlier document steps atomic. Recovery status is principal-bound, read-only
and budgeted, with optional original plan retrieval. Generic adapters can use it
without adding another focused MCP tool.

Derived status uses a bounded partial index over the ordinary frontmatter
projection, not whole-vault body scans. Transitive claim inspection caches at most
16 index snapshots, each capped at 1 MiB, with depth 8 and 256 visited claims/documents.
Unsupported index capabilities report unavailable; no background recomputation is
introduced. See [validation](agent-memory-validation.md) for exact commands,
measured context growth and the remote/native-desktop evaluation boundaries.
