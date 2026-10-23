# Agents use vault documents

In **Settings → Agents**, select vaults, permissions and an expiry to create a personal access token. From **Vault settings and sharing → Create a token for this vault**, the current vault is selected automatically. The secret is shown once; supply it separately as `NETSIDIAN_TOKEN` and send `Authorization: Bearer <token>`. Tokens inherit their creator's current vault role, intersected with explicit scopes and vault bindings. Revocation, role changes and removal take effect on subsequent requests. Read only is the default; trash permission is a separate choice.

## Memory folder and skill

In **Settings → MCP & agents → Agent memory & project knowledge**, select the
provider, deployment location, audience, scope identity and retention mode.
Netsidian, local Markdown and other remote providers have separate adapter guides;
a remotely hosted Netsidian instance still uses the Netsidian provider.

New configurations default visibly to **selective** retention of important
requests and durable outcomes. **Explicit only** retains only explicit memory
requests. Existing folder settings are unchanged until **Adopt memory policy** is
chosen. Configuration lives in this browser and in exported instructions; it is
not a server-wide interception or automatic-learning switch.

After adoption, **Prepare memory folder** creates a README if missing, preserving
existing documents. **Copy self-contained skill** includes the selected adapter
and full policy, while **Download skill package** includes references and the
POSIX local helper. Both exports contain destination data and no token. Supply
credentials separately. Read-only accounts can export instructions and recall;
they cannot prepare or write the destination. The connected token may have fewer
permissions than the current UI account.

The [portable policy package](../skills/agent-memory/SKILL.md) describes recall,
selective retention, exact wording, evidence, correction and read-only reflection.
Memory remains ordinary editable Markdown with normal history and permissions.
A folder is organization, not an access boundary. See the
[architecture decision](agent-memory-architecture.md) for provider limits,
compatibility, downgrade and recovery requirements.

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
