# MCP validation — 2026-10-12

## API audit

The live review deployment exposes **80 explicitly registered REST methods** and **44 implicit HEAD methods**. MCP discovery accounts for all **124**; missing methods: **0**. **93** are callable; the remaining operations are explicitly classified as human/session/provisioning workflows or continuous streams. This is inventory coverage, not permission to call an operation: membership, PAT vault bindings/scopes, federation policy and revision checks still apply.

The new exact-path metadata lookup closes an actual API gap: `GET /api/v1/spaces/{space}/documents/lookup?path=agent-memory/README.md` uses the indexed normalized path without reading the document body or fetching the vault list. Settings uses it when preparing the memory folder.

The default server and unit-test fixture have 76 registered methods plus 42 HEAD methods. The final review deployment additionally enables four local-folder methods and two HEAD aliases, all classified as human provisioning. Counts are instance-specific; the inventory is generated from enabled live routes.

## Ten actual iterations

Each row followed its implementation change and server rebuild. All calls used Streamable HTTP JSON-RPC MCP tools against an isolated, PAT-bound review vault. Each iteration created a Markdown note under `agent-memory/mcp-validation-2026-10-12/`, tagged `agent-memory`, read it, modified it with `If-Match`, replayed the same idempotency key without another revision, rejected a stale edit with **412**, and retrieved bounded tagged/folder context. After final deployment, bounded context was checked again for all ten IDs and matched every recorded modified revision. Rounds 5–10 also checked response projection; rounds 8–10 checked exact-path lookup; rounds 9–10 checked HEAD.

| Round | Implemented improvement | Workflow ms | Context JSON bytes* |
| --- | --- | ---: | ---: |
| 1 | Resolve shared path parameters and local schema references once at catalogue construction. | 71 | 616 |
| 2 | Bound REST response capture to 64 KiB by default; discard overflow while writing and report actual mutation status without suggesting an unsafe retry. | 147 | 599 |
| 3 | Separate read-only and mutation tools with enforced method guards and accurate read-only/idempotent annotations; keep the generic adapter for compatibility. | 107 | 606 |
| 4 | Preserve response MIME type and distinguish JSON, UTF-8 text and base64 binary instead of corrupting attachment bytes. | 134 | 598 |
| 5 | Add optional response field projection for reads so revision or metadata checks do not send an unnecessary document body to the agent. | 87 | 606 |
| 6 | Return correlation IDs and explicit conflict, rate-limit, authentication and uncertain-completion guidance without automatically retrying mutations. | 187 | 624 |
| 7 | Reject unknown path parameters, oversized query strings and invalid header values before dispatch; retain the existing traversal and credential forwarding guards. | 126 | 615 |
| 8 | Add indexed exact-path document metadata lookup and use it when preparing agent-memory instead of downloading the entire vault file list. | 248 | 601 |
| 9 | Discover implicit HEAD methods accepted by Go GET routes and suppress response bodies so header-only revision checks match real HTTP semantics. | 205 | 606 |
| 10 | Provide machine-readable setup configuration and limits, classify POST context retrieval as read-only, preserve the OAuth setup return link, and avoid advertising unreachable native app URLs. | 87 | 622 |

*Context sizes above are measured by the Python driver’s JSON serialization. The requested API budget was 4096 bytes; these are small fixtures, not a large-vault performance benchmark. Projection saved 503–563 bytes on the fixture reads. Recorded durations include the document workflow, not compilation or deployment. Two compile errors during refinement were corrected before their rounds passed; no failed workflow is counted as a completed round.

## Automated acceptance coverage

The HTTP/MCP suite checks live method coverage, anonymous setup isolation, shared parameters/resolved request schemas, cross-origin rejection, PAT scope enforcement and revocation, guarded edits, idempotent replay, traversal/extra path parameters/header injection, bounded writer capture including string writes, binary fidelity, projection, read/write method guards and empty HEAD bodies.

Playwright MCP checks login discovery, settings deep-link return after sign-in, copyable instructions and URL, permissions/lifetime controls, memory defaults, one-time token issuance/hiding/revocation, and the exact-path memory preparation flow. A development-to-Docker UID mismatch in the review volume’s history directories was corrected before the memory preparation check passed; development and production review processes now use UID 65532. Anonymous rate budgets are keyed by peer IP rather than TCP port, and invalid public-origin configuration returns a controlled error. Browser checks exercise an isolated review deployment; production user data is unchanged by test fixtures.

## Agent connection

Connect through `/mcp` and create a scoped PAT in Settings → MCP & agents. Run validation against a dedicated test vault.

## Desktop extension — 2026-10-13

Live loopback tests verify disabled/read-only defaults, token-free MCP negotiation, selected-vault isolation (including vault listing), write restrictions, unchanged PAT scopes, invalid-token rejection, cross-origin rejection, REST/administration isolation, port-conflict rollback, persistence and listener shutdown. HTTP and server-bootstrap suites pass. Playwright MCP verifies the real desktop controller and SPA in a disposable browser harness: enable access, select one vault, deny creation in read-only mode, allow creation after opting into writes, copy token-free instructions, and stop the listener. This is a desktop-mode browser harness, not physical GTK window automation. The native GTK/Wails package is built separately.
