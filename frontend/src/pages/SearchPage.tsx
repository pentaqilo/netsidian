import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import * as api from "../api/client";
import type { SearchHit } from "../api/types";

// The server's FTS5 snippet() is configured with '<mark>'/'</mark>' wrappers
// (see repository/sqlite/index.go: snippet(search_index, 4, '<mark>',
// '</mark>', '…', 18)). The old splitter only matched <b>/</b>, so <mark>
// tokens were left in the text and rendered literally. Split on <mark>
// (primary) and legacy <b> open/close tokens; matched terms become real
// <mark> elements, everything else renders as plain text — never inject the
// untrusted snippet via dangerouslySetInnerHTML.
function renderSnippet(snippet: string): ReactNode {
  const parts = snippet.split(/(<\/?mark>|<\/?b>)/gi);
  const out: ReactNode[] = [];
  let inMark = false;
  for (const part of parts) {
    const token = part.toLowerCase();
    if (token === "<mark>" || token === "<b>") {
      inMark = true;
      continue;
    }
    if (token === "</mark>" || token === "</b>") {
      inMark = false;
      continue;
    }
    if (!part) continue;
    if (inMark) {
      out.push(<mark key={out.length}>{part}</mark>);
    } else {
      out.push(<Fragment key={out.length}>{part}</Fragment>);
    }
  }
  return out;
}

export default function SearchPage() {
  const { spaceId = "" } = useParams();
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const timerRef = useRef<number | undefined>(undefined);

  useEffect(() => {
    window.clearTimeout(timerRef.current);
    if (!q.trim()) {
      setHits(null);
      setError(null);
      return;
    }
    timerRef.current = window.setTimeout(() => {
      api
        .search(spaceId, q)
        .then((r) => {
          setHits(r.hits);
          setError(null);
        })
        .catch((e: unknown) => {
          setError(e instanceof api.ApiError ? `${e.code}: ${e.message}` : String(e));
        });
    }, 300);
    return () => window.clearTimeout(timerRef.current);
  }, [q, spaceId]);

  return (
    <div className="pane">
      <h2>Search</h2>
      <input
        className="input"
        placeholder="Full-text search…"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        autoFocus
      />
      {error && <div className="banner error">{error}</div>}
      {q.trim() && hits === null && !error && <p className="muted">Searching…</p>}
      {hits !== null && hits.length === 0 && (
        <p className="muted">No results for “{q}”.</p>
      )}
      {hits?.map((h) => (
        <div className="hit" key={h.documentId}>
          <div className="title">
            <Link to={`/s/${spaceId}/d/${h.documentId}`}>{h.title || h.path}</Link>
          </div>
          <div className="path">{h.path}</div>
          <div>{renderSnippet(h.snippet)}</div>
        </div>
      ))}
      {hits === null && !q.trim() && (
        <p className="muted">Type to search this space.</p>
      )}
    </div>
  );
}
