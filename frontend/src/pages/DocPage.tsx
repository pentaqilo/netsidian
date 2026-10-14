import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams, useOutletContext } from "react-router-dom";
import * as api from "../api/client";
import type { Backlink, RevisionMeta } from "../api/types";
import { Editor, type SaveState } from "../components/Editor";
import { makeResolver, renderInto } from "../lib/markdown";
import type { ShellOutletContext } from "./Shell";

type Modal = "none" | "conflict" | "history" | "backlinks";

interface DocState {
  markdown: string;
  revision: string;
  canEdit: boolean;
  path: string;
  title: string;
}

interface Conflict {
  local: string;
  server: string;
  currentRevision: string;
}

const errText = (e: unknown) =>
  e instanceof api.ApiError ? `${e.code}: ${e.message}` : String(e);

export default function DocPage() {
  const { spaceId = "", docId = "" } = useParams();
  const { docs } = useOutletContext<ShellOutletContext>();
  const navigate = useNavigate();

  const [doc, setDoc] = useState<DocState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<"edit" | "preview">("edit");
  const [saveState, setSaveState] = useState<SaveState>("saved");
  const [dirty, setDirty] = useState(false);
  const [revEpoch, setRevEpoch] = useState(0);
  const [modal, setModal] = useState<Modal>("none");
  const [conflict, setConflict] = useState<Conflict | null>(null);
  const [revisions, setRevisions] = useState<RevisionMeta[] | null>(null);
  const [revView, setRevView] = useState<{ text: string; restoring: boolean } | null>(null);
  const [backlinks, setBacklinks] = useState<Backlink[] | null>(null);

  const revRef = useRef("");
  const textRef = useRef("");
  const docRef = useRef<DocState | null>(null);
  const previewRef = useRef<HTMLDivElement | null>(null);

  const statusLabel =
    saveState === "saving"
      ? "saving…"
      : saveState === "saved"
        ? dirty
          ? "unsaved"
          : "saved ✓"
        : saveState === "error"
          ? "error"
          : "unsaved";

  const reload = useCallback(async () => {
    setError(null);
    try {
      const n = await api.getDocument(spaceId, docId);
      const st: DocState = {
        markdown: n.markdown,
        revision: n.revision,
        canEdit: n.canEdit,
        path: n.path,
        title: n.title,
      };
      docRef.current = st;
      revRef.current = n.revision;
      textRef.current = n.markdown;
      setDoc(st);
      setRevEpoch(0);
      setMode("edit");
      setSaveState("saved");
      setDirty(false);
    } catch (e) {
      setError(errText(e));
    }
  }, [spaceId, docId]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Preview rendering: renderInto attaches its own click listeners to fresh
  // DOM on every call; React never touches the container's children.
  useEffect(() => {
    if (mode !== "preview") return;
    const el = previewRef.current;
    const d = docRef.current;
    if (!el || !d) return;
    const resolve = makeResolver(docs.map((x) => x.path));
    const open = (docPath: string) => {
      const hit = docs.find((x) => x.path === docPath);
      if (hit) navigate(`/s/${spaceId}/d/${hit.id}`);
      else window.alert(`No note at ${docPath} yet.`);
    };
    renderInto(el, d.markdown, resolve, open);
  }, [mode, doc, docs, navigate, spaceId]);

  const saveNow = useCallback(
    async (text: string, ifMatch: string): Promise<boolean> => {
      try {
        const res = await api.saveContent(spaceId, docId, text, ifMatch);
        revRef.current = res.revision;
        if (docRef.current) {
          docRef.current = { ...docRef.current, markdown: text, revision: res.revision };
        }
        setDoc((prev) => (prev ? { ...prev, markdown: text, revision: res.revision } : prev));
        return true;
      } catch (e) {
        if (api.ApiError.isConflict(e)) {
          const curRev =
            e.currentRevision ?? (await api.getDocument(spaceId, docId)).revision;
          const serverText = (await api.getDocument(spaceId, docId)).markdown;
          setConflict({ local: text, server: serverText, currentRevision: curRev });
          setModal("conflict");
        } else {
          window.alert(errText(e));
        }
        return false;
      }
    },
    [spaceId, docId],
  );

  const onSave = useCallback(async (): Promise<boolean> => {
    return saveNow(textRef.current, revRef.current);
  }, [saveNow]);

  const onSaveState = useCallback((s: SaveState) => setSaveState(s), []);
  const onChangeDirty = useCallback((d: boolean) => setDirty(d), []);
  const onText = useCallback((t: string) => {
    textRef.current = t;
  }, []);

  const keepMine = async () => {
    if (!conflict) return;
    const ok = await saveNow(conflict.local, conflict.currentRevision);
    if (ok) {
      setConflict(null);
      setModal("none");
      setSaveState("saved");
      setDirty(false);
    }
  };

  const loadServer = async () => {
    if (!conflict) return;
    try {
      const fresh = await api.getDocument(spaceId, docId);
      docRef.current = {
        markdown: fresh.markdown,
        revision: fresh.revision,
        canEdit: fresh.canEdit,
        path: fresh.path,
        title: fresh.title,
      };
      revRef.current = fresh.revision;
      textRef.current = fresh.markdown;
      setDoc(docRef.current);
      setRevEpoch((e) => e + 1); // remount Editor with server text
      setConflict(null);
      setModal("none");
      setSaveState("saved");
      setDirty(false);
    } catch (e) {
      window.alert(errText(e));
    }
  };

  const rename = async () => {
    if (!doc) return;
    const p = window.prompt("New path", doc.path);
    if (!p || p === doc.path) return;
    try {
      await api.renameDocument(spaceId, docId, p, revRef.current);
      await reload();
    } catch (e) {
      window.alert(errText(e));
    }
  };

  const trash = async () => {
    if (!window.confirm("Move this note to trash?")) return;
    try {
      await api.trashDocument(spaceId, docId, revRef.current);
      navigate(`/s/${spaceId}`);
    } catch (e) {
      window.alert(errText(e));
    }
  };

  const openHistory = async () => {
    setRevView(null);
    setRevisions(null);
    setModal("history");
    try {
      const r = await api.listRevisions(spaceId, docId);
      setRevisions(r.revisions);
    } catch (e) {
      window.alert(errText(e));
      setModal("none");
    }
  };

  const viewRevision = async (rev: string) => {
    try {
      const r = await api.getRevision(spaceId, docId, rev);
      setRevView({ text: r.markdown, restoring: false });
    } catch (e) {
      window.alert(errText(e));
    }
  };

  const restoreRevision = async () => {
    if (!revView) return;
    setRevView({ ...revView, restoring: true });
    try {
      const res = await api.saveContent(spaceId, docId, revView.text, revRef.current);
      revRef.current = res.revision;
      textRef.current = revView.text;
      const st: DocState = docRef.current
        ? { ...docRef.current, markdown: revView.text, revision: res.revision }
        : { markdown: revView.text, revision: res.revision, canEdit: true, path: "", title: "" };
      docRef.current = st;
      setDoc(st);
      setRevEpoch((e) => e + 1);
      setModal("none");
      setRevView(null);
      setSaveState("saved");
      setDirty(false);
    } catch (e) {
      window.alert(errText(e));
      if (revView) setRevView({ ...revView, restoring: false });
    }
  };

  const openBacklinks = async () => {
    setBacklinks(null);
    setModal("backlinks");
    try {
      const r = await api.getBacklinks(spaceId, docId);
      setBacklinks(r.backlinks ?? []);
    } catch (e) {
      window.alert(errText(e));
      setModal("none");
    }
  };

  if (error) return <div className="pane"><div className="banner error">{error}</div></div>;
  if (!doc) return <p className="muted">Loading…</p>;

  const docKey = `${docId}:${revEpoch}`;

  return (
    <>
      <div className="topbar">
        <span className="path">{doc.path}</span>
        <button
          className="btn small"
          onClick={() => setMode(mode === "edit" ? "preview" : "edit")}
        >
          {mode === "edit" ? "Preview" : "Edit"}
        </button>
        {doc.canEdit ? (
          <>
            <button className="btn small" onClick={() => void rename()}>Rename</button>
            <button className="btn small" onClick={() => void openHistory()}>History</button>
            <button className="btn small" onClick={() => void openBacklinks()}>Backlinks</button>
            <button className="btn small danger" onClick={() => void trash()}>Trash</button>
          </>
        ) : (
          <span className="muted">read-only</span>
        )}
        <span className={"status-dot " + (dirty && saveState !== "saving" ? "" : saveState)} title={dirty ? "unsaved" : saveState}>
          {statusLabel}
        </span>
      </div>

      {mode === "edit" ? (
        <div style={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}>
          <Editor
            docKey={docKey}
            initialText={doc.markdown}
            onSave={onSave}
            onChangeDirty={onChangeDirty}
            onSaveState={onSaveState}
            onText={onText}
          />
        </div>
      ) : (
        <div className="preview" ref={previewRef} />
      )}

      {modal === "conflict" && conflict && (
        <div className="modal-backdrop">
          <div className="modal">
            <h2>Edit conflict</h2>
            <p className="muted">The server has a newer revision. Choose what to keep.</p>
            <div className="muted">Server version:</div>
            <pre className="conflict-diff">{conflict.server}</pre>
            <div className="muted" style={{ marginTop: 8 }}>Your version:</div>
            <pre className="conflict-diff">{conflict.local}</pre>
            <div className="actions">
              <button className="btn" onClick={() => { setConflict(null); setModal("none"); }}>
                Cancel
              </button>
              <button className="btn" onClick={() => void loadServer()}>Load server version</button>
              <button className="btn primary" onClick={() => void keepMine()}>Keep mine (overwrite)</button>
            </div>
          </div>
        </div>
      )}

      {modal === "history" && (
        <div className="modal-backdrop">
          <div className="modal" style={{ width: 620 }}>
            <h2>History</h2>
            {!revisions && <p className="muted">Loading…</p>}
            {revisions && revisions.length === 0 && <p className="muted">No revisions yet.</p>}
            {revisions && revisions.length > 0 && !revView && (
              <table className="data">
                <tbody>
                  {revisions.map((r) => (
                    <tr key={r.id}>
                      <td>
                        <a href="#" onClick={(e) => { e.preventDefault(); void viewRevision(r.id); }}>
                          {r.id.slice(0, 8)}
                        </a>
                      </td>
                      <td className="muted">{r.creator}</td>
                      <td className="muted">{r.createdAt}</td>
                      <td className="right muted">{r.byteLength} B</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {revView && (
              <>
                <pre className="conflict-diff" style={{ maxHeight: 320 }}>{revView.text}</pre>
                <div className="actions">
                  <button className="btn" onClick={() => setRevView(null)}>Back</button>
                  <div className="spacer" />
                  <button
                    className="btn primary"
                    disabled={!doc.canEdit || revView.restoring}
                    onClick={() => void restoreRevision()}
                  >
                    {revView.restoring ? "Restoring…" : "Restore"}
                  </button>
                </div>
              </>
            )}
            {!revView && (
              <div className="actions">
                <button className="btn" onClick={() => setModal("none")}>Close</button>
              </div>
            )}
          </div>
        </div>
      )}

      {modal === "backlinks" && (
        <div className="modal-backdrop">
          <div className="modal">
            <h2>Backlinks</h2>
            {!backlinks && <p className="muted">Loading…</p>}
            {backlinks && backlinks.length === 0 && <p className="muted">No backlinks.</p>}
            {backlinks && backlinks.map((b) => (
              <div className="hit" key={b.documentId}>
                <div className="title">
                  <a
                    href="#"
                    onClick={(e) => {
                      e.preventDefault();
                      navigate(`/s/${spaceId}/d/${b.documentId}`);
                    }}
                  >
                    {b.title || b.path}
                  </a>
                </div>
                <div className="path">{b.path}</div>
              </div>
            ))}
            <div className="actions">
              <button className="btn" onClick={() => setModal("none")}>Close</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
