import {AppLogo} from '../components/AppLogo';
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { Link, NavLink, useNavigate, useParams, useOutlet } from "react-router-dom";
import * as api from "../api/client";
import type { DocMeta } from "../api/types";
import { useApp } from "../AppState";

export interface SpaceEvent {
  documentId?: string;
  seq: number;
  eventType: string;
}

export interface ShellOutletContext {
  spaceId: string;
  docs: DocMeta[];
  reloadDocs: () => void;
  lastEvent: SpaceEvent | null;
}

// ---- shared per-space doc store (keeps the list alive across route swaps) ----

interface DocsState {
  docs: DocMeta[];
  loading: boolean;
  error: string | null;
}

interface SpaceBox {
  // Replaced only by emit(), so the snapshot object keeps a STABLE reference
  // between renders (useSyncExternalStore requirement). Never construct a
  // fresh object inside getSnapshot.
  state: DocsState;
  // fetchDocs returns Promise<void> (snapshot lives in b.state), so the stored
  // in-flight marker must be typed Promise<void> to keep comparison/assignment consistent.
  promise: Promise<void> | null;
  lastKey: string;
  event: SpaceEvent | null;
  listeners: Set<() => void>;
}

const spaceCache = new Map<string, SpaceBox>();

function box(space: string): SpaceBox {
  let b = spaceCache.get(space);
  if (!b) {
    b = {
      state: { docs: [], loading: false, error: null }, // initial snapshot: docs [] (task B)
      promise: null,
      lastKey: "",
      event: null,
      listeners: new Set(),
    };
    spaceCache.set(space, b);
  }
  return b;
}

function emit(b: SpaceBox, patch: Partial<DocsState>) {
  b.state = { ...b.state, ...patch };
  b.listeners.forEach((l) => l());
}

function fetchDocs(space: string, key: string): void {
  const b = box(space);
  if (b.promise && b.lastKey === key) return;
  b.lastKey = key;
  emit(b, { loading: true, error: null });
  const p = api
    .listDocuments(space, key === "1")
    .then((r) => {
      if (b.lastKey === key) emit(b, { docs: r.documents, loading: false, error: null });
    })
    .catch((e: unknown) => {
      if (b.lastKey === key)
        emit(b, { loading: false, error: e instanceof api.ApiError ? `${e.code}: ${e.message}` : String(e) });
    })
    .finally(() => {
      if (b.promise === p) b.promise = null;
    });
  b.promise = p;
}

function touch(space: string, ev: SpaceEvent) {
  const b = box(space);
  b.event = ev;
  void fetchDocs(space, b.lastKey || "0");
}

// One EventSource per space shared by ALL mounted subscribers, plus a
// resubscription contract for useSyncExternalStore: React runs
// unsubscribe() before re-invoking subscribe() during every re-subscription
// (e.g. when a sibling's store subscription causes a re-render mid-route-
// swap). The returned unsubscribe is therefore made idempotent AND re-armable
// (subscribeSpace again re-arms the same live subscription), otherwise the
// component ends up with an inert cb and never sees store updates.
function subscribeSpace(space: string, cb: () => void) {
  const b = box(space);
  let attached = false;
  let es: EventSource | null = null;
  const ensureEs = () => {
    if (es) return;
    es = new EventSource(`/api/v1/spaces/${encodeURIComponent(space)}/events`, {
      withCredentials: true,
    });
    const onEvent = (ev: MessageEvent) => {
      try {
        const data = JSON.parse(ev.data as string) as Partial<SpaceEvent>;
        touch(space, {
          documentId: typeof data.documentId === "string" ? data.documentId : undefined,
          seq: typeof data.seq === "number" ? data.seq : 0,
          eventType: typeof data.eventType === "string" ? data.eventType : "unknown",
        });
      } catch {
        /* ignore malformed frames */
      }
    };
    // the server emits named "change" events; onmessage alone never fires for them
    es.addEventListener("change", onEvent);
    es.onerror = () => {
      /* EventSource retries automatically. */
    };
  };
  const subscribe = () => {
    b.listeners.add(cb);
    attached = true;
    ensureEs();
  };
  subscribe(); // arm immediately
  // Resubscribable closure: React's pattern unsubscribe() -> subscribe() with
  // the same subscribe fn (e.g. re-render during a route swap, or React 18+
  // StrictMode double-subscribe) must work — the returned function is BOTH
  // idempotent-unsubscribe and re-arm-on-next-call: attached toggles, the
  // first call detaches the cb, a later call reattaches and re-opens the SSE.
  const toggle = () => {
    if (attached) {
      attached = false;
      b.listeners.delete(cb);
      if (es) {
        es.close();
        es = null;
      }
    } else {
      subscribe();
    }
  };
  return toggle;
}

// Always holds the spaceId of the currently mounted Shell. Shell is the
// single mount point for this hook family (the only route rendering it), so
// one module ref suffices; useSpaceDocs keeps it fresh on every render.
const currentSpaceRef = { current: "" };

function useSpaceDocs(space: string, trashed: boolean) {
  currentSpaceRef.current = space;
  const key = trashed ? "1" : "0";
  // spaceRef always holds the CURRENT spaceId (updated on every render).
  // getSnapshot/subscribeSnap are created ONCE and read through it, so even
  // when React re-invokes the stable subscribeSnap during the :spaceId
  // change commit, it subscribes to the CURRENT space — no stale-space
  // re-arm, no inert cb, no frozen snapshot. Combined with the resubscribable
  // closure from subscribeSpace this removes the useSyncExternalStore
  // stale-subscription hazard that left the sidebar store never-updating.
  const spaceRef = useRef(space);
  spaceRef.current = space;
  const getSnap = useCallback(() => box(spaceRef.current).state, []);
  const subscribeSnap = useCallback(
    (cb: () => void) => {
      const sub = subscribeSpace(spaceRef.current, cb);
      return () => sub(); // close over the CURRENT space's subscription
    },
    [],
  );
  const state = useSyncExternalStore(subscribeSnap, getSnap, getSnap);
  useEffect(() => {
    void fetchDocs(space, key);
  }, [space, key]);
  return state;
}

// ---- layout ----

function MainColumn({ spaceId, docs, reloadDocs, lastEvent }: ShellOutletContext) {
  // useOutlet is NOT generic in react-router-dom v7.18 (returns
  // ReactElement | null); the context object is passed as its first argument.
  const element = useOutlet({ spaceId, docs, reloadDocs, lastEvent });
  return (
    <main className="main">
      <div className="content">{element}</div>
    </main>
  );
}

export default function Shell() {
  const { spaceId = "" } = useParams();
  const { me, spaces, signOut, refresh } = useApp();
  const navigate = useNavigate();
  const [showTrashed, setShowTrashed] = useState(false);
  const [spaceError, setSpaceError] = useState<string | null>(null);
  const { docs, loading, error } = useSpaceDocs(spaceId, showTrashed);
  const lastEvent = spaceCache.get(spaceId)?.event ?? null;

  // DEFECT 1 root cause: newNote() dereferenced the `docs` binding coming
  // out of useSyncExternalStore — when the subscription lagged a store
  // update (space swap / re-subscribe race) that binding could be undefined
  // while the store itself still held the live list the tree rendered.
  // liveDocs() is the single live source: the store box's CURRENT snapshot
  // for the mounted space, with a non-array guard falling back to []. Both
  // the sidebar tree and newNote read it, so the default filename is always
  // derived from the live document count and nothing dereferences undefined.
  const liveDocs = useCallback((): DocMeta[] => {
    // store's CURRENT snapshot — the same source the sidebar tree renders
    const s = box(currentSpaceRef.current).state;
    // undefined/null guard: a torn-down or stale subscription can surface a
    // non-array snapshot; box() only ever creates initial state with
    // docs: [] and emit() only spreads it, so the safe fallback is [] and
    // the live count stays coherent with what the tree shows.
    return Array.isArray(s.docs) ? s.docs : [];
  }, [spaceId]);

  const reloadDocs = useCallback(() => {
    void fetchDocs(spaceId, showTrashed ? "1" : "0");
  }, [spaceId, showTrashed]);

  const errText = (e: unknown) =>
    e instanceof api.ApiError ? `${e.code}: ${e.message}` : String(e);

  const newNote = async () => {
    // live source only — never the raw snapshot binding (see liveDocs above)
    const current = liveDocs();
    const n = current.length + 1;
    const path = window.prompt("New note path", `note-${n}.md`);
    if (!path) return;
    try {
      const meta = await api.createDocument(spaceId, path, "# Title\n");
      reloadDocs();
      // navigate runs even if createDocument ever returns without an id —
      // prevents the "URL stayed at /s/{space}" after-success regression.
      navigate(`/s/${spaceId}/d/${encodeURIComponent(meta.id)}`);
    } catch (e) {
      window.alert(errText(e));
    }
  };

  const restore = async (id: string) => {
    try {
      await api.restoreDocument(spaceId, id);
      reloadDocs();
    } catch (e) {
      window.alert(errText(e));
    }
  };

  const createSpace = async () => {
    const name = window.prompt("New space name");
    if (!name) return;
    setSpaceError(null);
    try {
      await api.createSpace(name, "personal");
      await refresh();
    } catch (e) {
      setSpaceError(errText(e));
    }
  };

  const doSignOut = async () => {
    await signOut();
    navigate("/auth", { replace: true });
  };

  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="head">
          <AppLogo size={24}/> Netsidian
        </div>
        <div className="body">
          <nav>
            <NavLink className="nav-item" to={`/s/${spaceId}/search`}>Search</NavLink>
            <NavLink className="nav-item" to={`/s/${spaceId}/graph`}>Graph</NavLink>
            <NavLink className="nav-item" to="/settings">Settings</NavLink>
          </nav>
          <div className="section">
            <span>Spaces</span>
            <button className="btn small" onClick={() => void createSpace()} title="Create personal space">
              +
            </button>
          </div>
          {spaceError && <div className="banner error">{spaceError}</div>}
          <ul className="tree">
            {spaces.map((sw) => (
              <li key={sw.space.id}>
                <Link
                  className={sw.space.id === spaceId ? "active" : undefined}
                  to={`/s/${sw.space.id}`}
                >
                  {sw.space.name}
                </Link>
              </li>
            ))}
          </ul>
          <div className="section">
            <span>Documents</span>
            <button className="btn small" onClick={() => void newNote()}>
              + Note
            </button>
          </div>
          <label className="muted" style={{ display: "block", padding: "0 10px 6px", fontSize: 12 }}>
            <input
              type="checkbox"
              checked={showTrashed}
              onChange={(e) => setShowTrashed(e.target.checked)}
            />{" "}
            show trashed
          </label>
          {loading && <p className="muted" style={{ padding: "0 10px" }}>Loading…</p>}
          {error && <div className="banner error">{error}</div>}
          <ul className="tree">
            {liveDocs().map((d) => (
              <li key={d.id} style={{ display: "flex", alignItems: "center", gap: 4 }}>
                <Link
                  className={window.location.pathname === `/s/${spaceId}/d/${d.id}` ? "active" : undefined}
                  style={{ flex: 1, minWidth: 0 }}
                  to={`/s/${spaceId}/d/${d.id}`}
                >
                  {d.title || d.path}
                </Link>
                {d.tombstoneAt && (
                  <button className="btn small" onClick={() => void restore(d.id)}>
                    Restore
                  </button>
                )}
              </li>
            ))}
          </ul>
        </div>
        <div className="foot">
          <span>{me?.displayName}</span>
          <button className="btn small" onClick={() => void doSignOut()}>
            Sign out
          </button>
        </div>
      </aside>
      <MainColumn spaceId={spaceId} docs={docs} reloadDocs={reloadDocs} lastEvent={lastEvent} />
    </div>
  );
}
