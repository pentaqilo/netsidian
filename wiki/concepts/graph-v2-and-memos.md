---
title: Graph v2 & Memos
created: 2026-10-12
updated: 2026-10-12
type: concept
tags: [graph, api, vault]
sources: [raw/notes/code-scan-2026-10-12.md]
confidence: high
---

# Graph v2 & Memos

Three retrieval surfaces added alongside the legacy graph and the v1 analysis
([[links-and-graph]]): a lightweight full link index, a memo query, and a tag
listing.

## `GET /spaces/{space}/graph2` (`service/graph2.go`)
A **complete, lightweight** link index. Deliberately *no* community analysis,
*no* content reads, and *no* 1000-node sampling — that is the whole difference
from `graph/analysis` v1 ([[links-and-graph]]). Nodes carry `kind: document |
unresolved` (an unresolved target id is `unresolved:<sha20>` of the normalized
path); edges are deduped and sorted; each node reports its link count ^[raw/notes/code-scan-2026-10-12.md].

## `GET /spaces/{space}/memos` (`service/memos.go`)
Paginates **memo-tagged documents by creation time**. Filters: `folder` (omitted
= all, empty = vault root, otherwise descendants), `tag`, `q` (literal search in
indexed memo content/title/path, ≤ 400), and a creation date range (`from`
inclusive / `before` exclusive). `limit` defaults to 20, max 50. The opaque
cursor is bound to the vault **and** the active filters — reusing it under a
different filter returns 400. Responses include revision/tags/creation time, the
matched total, available tag counts, and `nextCursor`
^[raw/notes/code-scan-2026-10-12.md].

## `GET /spaces/{space}/tags`
Lists tags aggregated from the `document_tags` index ([[persistence]]) — the
same tag vocabulary the memo and context filters accept ([[agent-interface]]).

## Related
[[links-and-graph]] · [[http-api]] · [[markdown-pipeline]] · [[vaults-and-spaces]]
