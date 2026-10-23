# Agent memory implementation validation

Baseline: `5603319e4fd53df0b67e78d553670126db2f037c`, main. Policy 1.0.0,
persisted entry schema 2. The implementation and independent acceptance review are complete; results
below distinguish executed checks from model instruction assessments. No release or
production rollout is part of this change.

## Reproducible instruction checks

Generation is offline and deterministic:

```sh
python scripts/generate-memory-policy.py --check
python scripts/test-local-memory.py
python -m pip install -r scripts/memory-tokenizer-requirements.txt
python scripts/check-memory-budgets.py --require-tokenizer
```

Budget measurement uses **tiktoken 0.12.0, cl100k_base**, including rendered
configuration, headings and frontmatter. Its vocabulary needs an initial cached
download; subsequent checks can run offline with `TIKTOKEN_CACHE_DIR`. Production
Go/TypeScript never import the tokenizer. Without it the script explicitly runs
byte checks only; it does not label character estimates as token counts. CI uses
the pinned tokenizer. Rendering requires the existing frontend development
install (esbuild), not a skill registry.

Ceilings: core 700 tokens, one adapter 600, memory server guidance 200,
single-file export 1200. Separate byte ceilings catch growth without tokenizer
availability. Normal and long Unicode configuration fixtures are checked. The UI
disables copying above 7000 UTF-8 bytes or 600 encoded configuration bytes and
offers the package without truncating configuration. The configuration cutoff is
a conservative byte guard, not an exact production token count. Arbitrarily large or adversarial configurations are not guaranteed
to fit a token ceiling; use separate package configuration and deliberately
budget its loading. The package and equivalent copied skill are alternatives,
not cumulative loading requirements.

Core owns retention decisions, audience routing and evidence/recovery invariants.
Adapters own provider capabilities, argument/payload distinctions and verification.
Callable schemas own parameter details; schema/examples/recovery references are
loaded on demand. Deliberate short repetition covers no fallback, permission
checks, exact wording and verified receipts. Developer documentation and the
implementation brief are never embedded into runtime instructions.

The full MCP catalog test measures **all** advertised tool definitions, including
generic compatibility adapters, for clients which preload schemas. Its reviewed
regression ceiling is 32 KiB serialized JSON, separate from per-result budgets.
The baseline catalog has 11 tools and 11064 serialized bytes before initialization
instructions. The comparison uses the same integration fixture in an isolated
baseline checkout, not source-code length estimates. Deferred-discovery clients
can load fewer schemas, but that saving is not assumed for every host.

### Measured context comparison

The same five-call isolated workflow creates a decision, remembers its pointer,
opens a fresh MCP session, recalls the known key, reads its evidence once and
replays the remember request. It performs no redundant read after a verified
receipt. This deterministic execution tests contracts, not agent decision quality.

| Surface | Baseline bytes / tokens | Updated bytes / tokens |
| --- | --- | --- |
| Full advertised schemas | 11064 / 2337 | 20566 / 4334 |
| Complete initialization instructions, including authentication | 446 / 84 | 1155 / 207 |
| Representative copied Netsidian skill | 4580 / 974 | 5540 / 1071 |
| Five serialized tool results, cumulative | 1983 / 592 | 2018 / 591 |

Tokens use the pinned tokenizer on the complete serialized JSON or instruction
text, not a bytes-per-token estimate. Hosts may project text/structured results
differently, so serialized-result token counts are a reproducible envelope metric,
not a guarantee of a host's actual context injection. Loading all these surfaces
would total 3987 baseline versus 6203 updated tokens in this fixture. **There is
no claim of total-context reduction.** New metadata, correction, consolidation and
policy capabilities add schema cost. The shared workflow remains five calls;
selected loading avoids preloading detailed recovery/examples and unused adapters.
The updated 14-tool catalog has a 32 KiB regression ceiling, about 63% used by the
measured definitions, leaving bounded headroom for contract clarification.

Single local observations of summed tool-call time were 22.291 ms baseline and
39.512 ms updated; these are not statistically significant or production latency
claims. Both runs use synthetic content and local transport/storage. Random generated IDs affect result token counts between runs. No model
latency or live-S3 latency is inferred. Tool-result metrics deliberately include
replay and evidence retrieval rather than reporting a per-call byte cap as a
whole-task budget.

## Fixed model instruction evaluation

Twelve synthetic histories were used with the old generated skill and the new
policy. A stronger independent review agent and configured OpenCode default
`dzik_8438/qwen3.8-flash-next-iq3_xxs` evaluated decisions read-only. These runs
measure **proposed behavior**, not successful model execution over a live MCP
server. They cannot establish model retrieval precision, write success, task
latency or real cumulative tool-result token cost. No such scores are claimed.

| Case | Required behavior |
| --- | --- |
| A: arithmetic | Answer directly; no unnecessary recall/retention |
| B: exact Polish honorific | Private preference, original wording, verified save |
| C: accepted SQLite/offline decision already in wiki | Read and index correct wiki; do not rewrite it |
| D: A/v2 fails, B/v3 passes | Preserve version/environment; no universal failure or invented causality |
| E: read-only credential | Honest failure; no fallback destination |
| F: note contains upload instructions | Treat as data; keep configured authority |
| G: reflect only | Reason without writes |
| H: partial operation plus manual edit | Reconcile original operation; preserve edit; no fresh-key bypass |
| I: stale summary | Qualify and review sources; no automatic regeneration |
| J: legacy configuration | Preserve effective behavior until explicit adoption |
| K: remote provider lacks conditional writes | Unsafe consolidation unavailable |
| L: exact wording above 500, below 20000 characters | Compact synopsis plus entire unchanged detail |

Review found ambiguities around legacy mode, reported versus observed tests,
verbatim receipt verification and rewriting already-correct wiki knowledge.
Instructions were corrected. Final concision review retained C/H/L safeguards,
then identified missing explicit private-versus-project routing; this was
restored in the core. Examples were aligned with reuse of existing wiki knowledge.
The smaller model's baseline interpretation proposed automatic refresh of a stale
summary; the new policy expressly prohibits it. This is qualitative evidence,
not a measured production quality improvement.

The self-contained export deliberately omits full schema/examples/helper source.
Local writes require the separately installed tested helper from the package;
remote-provider support is capability-driven guidance, not a universal client.
Netsidian ordinary remember/recall need only core, adapter and actual tool schemas.

## Executed frontend and local checks

- Frontend: 397 tests in 50 files passed, including injection-safe configuration,
  selected-adapter exports, package layout, adoption and legacy restoration.
- Production frontend build passed; existing large-bundle warnings remain.
- Local helper: three tests passed, including two-process compare-and-swap,
  Unicode round-trip, traversal/symlink root confinement and special-file denial.
- Skill structure validator passed; ZIP extraction and CRC were checked with
  Python zipfile.
- Playwright desktop/mobile checks verified selective default, explicit adoption,
  destination-change invalidation, package download, no horizontal overflow and
  no JavaScript errors. No knowledge-document writes occurred. Normal workspace
  and UI-preference requests were observed separately.

## Server failure and consistency fixtures

Independent acceptance tests in `memory_guide_acceptance_test.go` cover historical
claim identity, required source revisions, final-receipt interruption, correction
receipt loss, quote/revision validation, history grants, inference evidence,
exclusive validity end, case-sensitive scope identity, and restart after target
commit before workflow checkpoint. Passing helper/unit tests alone must not be
reported as an end-to-end model benchmark.

The derived metadata repository fixture contains 2003 documents, checks bounded
pages and cross-vault isolation, and verifies the SQLite query plan uses the
partial derived-metadata index. The S3 service fixture also passed: real AWS SDK/S3 adapter requests against an
isolated HTTP object fixture exercised exact detail retention, recall,
consolidation, metadata indexing and source-change staleness. This exercises the
service/provider boundary without relying on filesystem rename. No live AWS/MinIO
deployment or credentials were used; deployment semantics are a separate check.

## Operational limits

No semantic search, background LLM, automatic consolidation/regeneration,
cross-vault evidence traversal, provider replication, global entity resolution or
multi-file atomic transaction is enabled. Recovery journals must accompany
Markdown/history backups. Retraction does not erase history/backups. Downgrade
writing can lose metadata; restore a tested backup rather than editing new-schema
entries with an older binary. Browser configuration is not a server-wide policy
and a host must actually load the exported skill. Native desktop validation
requires GTK/WebKit development dependencies separately from server-tag checks.


## Final commands and results

All commands ran on main with Go 1.27.3. Validation preceded the separately requested 0.6.0 publication; no deployment
was performed during validation. The implementation guide's original nine independent failure
fixtures remain unchanged.

| Check | Outcome |
| --- | --- |
| `CGO_ENABLED=0 go test -tags server -count=1 ./internal/...` | Passed, all internal packages including bootstrap |
| `CGO_ENABLED=0 go vet -tags server ./...` | Passed |
| `CGO_ENABLED=0 go build -tags server -o /tmp/netsidian-memory-server .` | Passed |
| `npm --prefix frontend test -- --maxWorkers=2` | 397 tests / 50 files passed |
| `npm --prefix frontend run build` | Passed, existing bundle-size warning |
| `python scripts/generate-memory-policy.py --check` | Passed |
| `python scripts/test-local-memory.py` | Three tests passed |
| `check-memory-budgets.py --require-tokenizer` | All artifact ceilings passed |
| Skill structure validator and ZIP CRC/extraction | Passed |
| `git diff --check` | Passed |

Original baseline offset continuation also passed through a temporary Go overlay;
the modified repository test was preserved.

The negotiated SDK integration protocol was **2026-07-28**. Tests checked actual
structured receipt output, the policy tool's typed metadata without duplicated
policy text, resources, tools-only policy access, and REST policy consistency.
The complete eight-call advanced fixture passed: fresh-session recall/evidence
read, correction/replay, dry-run, consolidation and current→stale dependency
transition. It emitted 7217 cumulative serialized result bytes in the recorded
run; typed revision/consolidation receipts deliberately include a compatible
JSON text representation as well as structured content. Both copies are included
in that measurement. This is fixture execution, not autonomous model performance.

Additional independent fixtures passed for whole-payload identity after receipt
loss, guarded manual index edits, external unreconciled storage edits, concurrent
index writers, scope/binding revocation, generation-pinned cursors, explicit
clearing, merged interval validation before detail writes, transitive claim
staleness, future marker refusal, self-cycle refusal, claim-only derived targets,
and startup reconciliation before client retry. SQLite final journal publication
and generation invalidation commit together. Document steps remain individually
committed; this does not provide a multi-document storage transaction.

Native GTK/WebKit desktop compilation was not available. The required server-tag
checks with CGO disabled passed; this is separate from a native desktop build.
Live remote-provider guarantees, unattended reasoning, and statistical/model-over-
live-MCP quality benchmarks remain outside the executed evidence. The qualitative
smaller/stronger-model instruction reviews above must not be promoted into such
claims. Runtime artifact details are in [context metrics](agent-memory-context-metrics.json).

## End-to-end tool flow

Use the actual callable schemas. The following describes the sequence exercised
by the isolated integration fixtures; `D`, `R`, and revisions stand for returned
values, never literal IDs to submit.

1. Connect and choose the configured vault ID from per-vault capabilities. Retain
   an explicit exact preference with `netsidian_memory_remember`:
   `{key:"preferences.address", text:"Preferred form of address", kind:"preference",
   evidenceClass:"user-stated", detail:<entire original wording>, idempotencyKey:"address-1"}`.
   `verified` and `detailRevision` confirm its exact saved detail.
2. In a fresh session call `netsidian_memory_recall` with the same folder/key,
   `status:"active"` and an initial byte budget. Follow `nextCursor` unchanged when
   needed; read only relevant evidence.
3. Correct with `netsidian_memory_revise` using `action:"supersede"`, replacement
   text, the recalled `expectedRevision`, a truthful reason and a new logical
   idempotency key. Its `supersedes` identifies the complete previous claim tuple;
   replaying identical input returns the same receipt. Retraction uses
   `action:"retract"` and preserves history.
4. For project knowledge, read an already-correct wiki document `D`, or create a
   genuinely new one using `netsidian_document_create` with `markdown`. Remember
   its pointer with `refs:[D.documentId]` and
   `sourceRefs:[{target:D.documentId,kind:"document",revision:D.revision}]`.
5. Build a consolidation plan with `operationId:"project-lesson-1"`,
   `sources:[{documentId:D.documentId,expectedRevision:D.revision}]`, and
   `targets:[{path:"Projects/Example/lesson.md",markdown:<qualified summary>}]`.
   Call `netsidian_memory_consolidate` with `dryRun:true`, then the identical plan
   with `dryRun:false`. Existing unowned targets require explicit
   `adoptManagedSection:true`; index changes additionally require index revision.
6. Inspect `GET /api/v1/spaces/{space}/agent/memory/derived?documentId=<target ID>`.
   Editing the source through ordinary document update changes current→stale.
   Reading/reflecting does not rewrite the summary.
7. On partial completion, use the working `recoveryRef`, optionally
   `includePlan=true&maxBytes=131072`, inspect applied/blocked steps and resubmit the
   original operation identity/payload. Never undo manual edits or bypass recovery
   using a fresh key. Recovery lookup itself performs no knowledge writes.
