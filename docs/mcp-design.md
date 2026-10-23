# MCP design and deployment

For the reviewed knowledge/project workflow roadmap and company-use gaps, see
[MCP for knowledge and project work](mcp-agent-workflows.md). It distinguishes
implemented adapter behavior from proposed domain tools.

## Decisions

| Area | Implementation | Reason |
| --- | --- | --- |
| Transport | Official Go SDK, Streamable HTTP `/mcp`, stateless JSON responses | Reuse protocol negotiation/validation; avoid another process, session affinity and per-agent session state. |
| Identity | Explicit bearer PAT, existing expiry/revocation/account/federation checks | One authorization model for REST and MCP; browser cookies cannot confer agent access. |
| Setup | Public `netsidian_connect`; login/start and settings expose the installation message | Agents can discover the token setup URL without reading private data. |
| API coverage | Live route registry plus implicit HEAD; in-process dispatch through the same HTTP handlers | New registered API methods are picked up automatically; no duplicate business logic or loopback network hop. |
| Discovery | Filtered/paged catalogue; inspect one operation; resolve schemas at startup | Keep tool definitions small and avoid transferring the full OpenAPI contract for every action. |
| Retrieval | Existing bounded `/agent/context`, paged metadata, indexed path lookup, optional field projection, HEAD | Retrieve relevant metadata/snippets first; fetch full bodies only when needed. |
| Mutations | Preserve `If-Match`, idempotency keys and REST error/status information | Prevent lost edits and duplicate writes; conflicts require rereading and merging. |
| Payloads | 32 MiB requests, 64 KiB default/256 KiB maximum captured responses, MIME-aware binary encoding | Bound adapter memory; report omitted responses without hiding operation completion. |
| Tools | Separate read-only and mutation tools; retain generic compatibility adapter | Give clients accurate confirmation hints while enforcing the same distinction on the server. |
| Memory | Ordinary `agent-memory/` Markdown tagged `agent-memory` | Portable, inspectable, searchable content with existing history and access checks. |

## Request flow

1. Middleware checks Origin/Host, rejects cross-site requests, validates a supplied bearer token and applies a per-principal request budget (peer IP for anonymous setup, independent of TCP port).
2. Anonymous calls see only setup. Authenticated calls use the SDK’s generated input schemas.
3. Discovery selects a registered operation. Dispatch rejects unavailable human/stream workflows, unexpected path parameters, traversal, oversized queries and malformed headers.
4. The adapter constructs an internal request with the original PAT and existing revision/idempotency headers. It forwards no browser cookies or caller-selected outbound URL.
5. The normal REST handler performs current permission checks and service work. A bounded response writer captures status, headers and body.
6. The tool returns JSON status/body metadata, MIME/encoding, ETag, Retry-After and recovery guidance. It never automatically retries a mutation.

## Authentication profile

This release supports scoped PATs. It does **not** claim to implement an OAuth authorization server or dynamic client registration. Existing OIDC/federation supplies browser login and policy revalidation; a user then issues a scoped, expiring PAT for the agent. OAuth-only MCP clients need manual bearer-header support to use this profile. Adding an OAuth authorization server later would require protected-resource metadata, audience-bound tokens, PKCE and a complete consent/client-registration policy; publishing incomplete metadata now would mislead clients.

Prefer the smallest vault set, read-only scope where possible, and a short expiration. Explicitly select write/trash capabilities when needed. Store credentials in the client’s secret store or an environment variable; avoid token-bearing URLs, committed configs and Markdown. Revocation takes effect on the next request. Folder/tag conventions do not impose a security boundary within a PAT-authorized vault.

## Deployment

The Docker server exposes `/mcp` on its normal application port; no separate listener or sidecar is required. Remote deployments use HTTPS and `NETSIDIAN_PUBLIC_URL=https://your-netsidian.example`; reverse proxies preserve Host and do not buffer continuous application SSE streams. MCP itself uses bounded JSON replies and does not require a persistent SSE session. The SDK retains localhost DNS-rebinding protection when no public origin is configured; configured origins pin the accepted Host/Origin.

The desktop app has an opt-in, MCP-only listener on `127.0.0.1`. Its internal asset-server URL remains private. Settings persist the selected vaults, port, token-free option, read-only mode and optional trash permission. The listener starts/stops with the app; an occupied new port does not replace a working listener.

Token-free local access constructs an in-memory scoped actor and reuses normal service authorization without issuing a stored PAT or granting the local principal’s full privileges. Valid PATs are intersected with the configured vaults/scopes; an empty scope intersection is denied, never interpreted as unrestricted access. Anonymous PAT-required setup exposes only installation instructions. Token-free setup returns a URL-only client configuration. The listener serves no REST, UI or administration routes. Host/Origin validation and loopback peer checks remain mandatory.

Desktop settings control endpoints exist only in the internal native API and reject agent keys. Configuration defaults to disabled/read-only with no selected vaults. Default memory folder creation is explicit through settings and does not move existing notes.

## Validation evidence

[Ten refinement rounds and acceptance tests](mcp-validation.md) document actual tool calls and their limits.

## Standards

- [MCP Streamable HTTP](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports): JSON-RPC, negotiation, Origin validation and HTTP behavior.
- [MCP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization): the OAuth authorization model for a future OAuth-enabled profile; this release explicitly uses manual PAT authentication.
- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk): protocol implementation rather than handwritten protocol state.
- [Codex MCP configuration](https://developers.openai.com/codex/mcp/): Streamable HTTP server URLs and bearer-token environment configuration.

## Evidence-backed portable policy

The versioned source is `skills/agent-memory/`; deterministic offline generation
projects it into Go and TypeScript. The settings UI exports either a self-contained
selected-adapter skill or a complete ZIP. Adoption is explicit for existing
configurations. Provider and deployment location remain independent; no runtime
LLM extraction or fallback storage is enabled. See
[agent-memory-architecture.md](agent-memory-architecture.md) for the authority,
evidence, compatibility and non-atomic recovery decision.
