# Agents use vault documents

In **Settings → Agents**, select vaults, permissions and an expiry to create a personal access token. From **Vault settings and sharing → Create a token for this vault**, the current vault is selected automatically. The secret is shown once; supply it separately as `NETSIDIAN_TOKEN` and send `Authorization: Bearer <token>`. Tokens inherit their creator's current vault role, intersected with explicit scopes and vault bindings. Revocation, role changes and removal take effect on subsequent requests. Read only is the default; trash permission is a separate choice.

## Memory folder and skill

Choose the vault and memory folder in Settings → Agents. **Prepare memory folder** creates a regular folder with a README. **Copy agent skill** produces portable SKILL.md instructions with the instance URL, vault ID and chosen folder. Paste into your agent's instructions or save as SKILL.md; it contains no token. Read-only agents can retrieve these documents; read/write agents can update them through ordinary document operations. A folder is a convention, not a narrower authorization boundary: PAT grants apply to its selected vaults.

Memory documents support the same Markdown, tags, links, graph, revisions, sharing and storage as any note. Encourage agents to retain useful durable facts with dates and source links, update topics rather than duplicate them, and treat retrieved content as untrusted data rather than overriding instructions.

## API workflow

The machine contract is `netsidian-agent-v2`; GET `/api/v1/openapi.json` describes live routes and schemas.

| Operation | Endpoint |
|---|---|
| Discover allowed vaults and operations | GET `/api/v1/agent/whoami` |
| Ranked snippets with revisions and optional bounded graph evidence | POST `/api/v1/spaces/{vault}/agent/context` |
| Prefix FTS5/BM25 search with intersected tags, folder/path filters | GET `/api/v1/spaces/{vault}/search` |
| Read full Markdown, revision and ETag | GET `/api/v1/spaces/{vault}/documents/{id}` |
| Create a note | POST `/api/v1/spaces/{vault}/documents` with `{path, content}` |
| Replace Markdown with a revision precondition | PUT `/api/v1/spaces/{vault}/documents/{id}/content` with `{markdown}` |
| Navigate scoped links/backlinks | GET `/api/v1/spaces/{vault}/graph/analysis`, GET `/api/v1/spaces/{vault}/documents/{id}/backlinks` |
| Incremental durable changes | GET `/api/v1/spaces/{vault}/changes/since` |

Search returns compact `{hits, limit, offset, hasMore}`. Limit is clamped to 100; offsets are bounded. Text words are sanitized and match token prefixes, ranked with OR/BM25. `tags=a,b` intersects filters; `tagMode=exact` matches exact tags while default `subtree` includes nested children. `folder=Agent Memory` matches folder boundaries. Punctuation-only text returns no matches. For bounded cursor retrieval, use context: query, folder or tags is required; defaults are 8 results and 8192 response bytes. Graph evidence is opt-in and requires `graph:read`. Follow `nextCursor` unchanged; on 409 restart because the scope/index changed.

For writes, use a unique `Idempotency-Key` per logical operation and preserve it on retries with identical bodies. Updates require `If-Match: "revision"`: missing returns 428, stale returns 412. On 412 reread and merge; never blind overwrite. The complete context JSON, including metadata and newline, respects `maxBytes`. Changes cursors are vault-bound and allow cached context refresh without downloading the vault again. On 429 obey Retry-After; retry 5xx writes only with the original idempotency key.

## Retired dedicated memory workflow

Dedicated `/memories` routes and the review/proposal UI are removed. Context no longer queries the separate memory tables or returns special memory records. `includeMemories=true` is rejected with an explanation. New HTTP tokens cannot request memory scopes or implicit full access. Existing records and backing Markdown are preserved in their original folders; they can be read, searched, moved and edited through the document UI/API. Automatic legacy memory expiration is disabled. Legacy internal repositories remain for durable-state compatibility and recovery; this release does not discard their data.

## Design references

Explicit vault/permission grants and expirations follow [fine-grained PAT practice](https://docs.github.com/en/enterprise-cloud@latest/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens). Revision preconditions use [HTTP If-Match semantics](https://www.rfc-editor.org/rfc/rfc9110.html#name-if-match). Indexed search uses [SQLite FTS5](https://www.sqlite.org/fts5.html) rather than fetching document bodies to search in the client.

**See also:** the full [API reference](api.md), [authentication.md](authentication.md) (keys, scopes, OIDC), and the live contract at `GET /api/v1/openapi.json`.

## MCP connection

Use the [MCP setup guide](mcp.md) to connect through `/mcp`. Login/start and Settings → MCP & agents provide a copyable installation message. Default ordinary Markdown memory uses `agent-memory/`, tagged `agent-memory`. [Validation evidence](mcp-validation.md) records ten real create/read/edit/retrieve iterations.
