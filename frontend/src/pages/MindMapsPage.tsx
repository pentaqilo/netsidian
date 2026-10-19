// MindMapsPage: the "Maps" tool tab. Lists every folder-backed mind map in
// the vault, creates new blank canvases, and opens them as workspace tabs.

import { useRef, useState } from 'react';
import * as api from '../api/client';
import { Dialog } from '../components/Dialog';
import { Icon } from '../components/Icon';
import { mindMapPathError } from '../lib/mindmaps';

const errMsg = (e: unknown) => e instanceof Error ? e.message : 'Something went wrong.';
const mapCount = (n: number) => `${n} ${n === 1 ? 'map' : 'maps'}`;

export type MindMapsStatus = 'loading' | 'ready' | 'error';

export default function MindMapsPage({ space, mindMaps, folders, writable, mapsStatus = 'ready', mapsError = '', onRetryMaps, onOpenMap, onChanged }: {
  space: string;
  mindMaps: api.MindMapSummary[];
  folders: string[];
  writable: boolean;
  mapsStatus?: MindMapsStatus;
  mapsError?: string;
  onRetryMaps?: () => void;
  onOpenMap: (path: string) => void;
  onChanged: () => void;
}) {
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');

  const loading = mindMaps.length === 0 && mapsStatus === 'loading';
  const failed = mindMaps.length === 0 && mapsStatus === 'error';
  const stale = mindMaps.length > 0 && mapsStatus === 'error';
  const empty = mindMaps.length === 0 && mapsStatus === 'ready';
  const create = () => { setError(''); setCreating(true); };

  return <div className="boards-page">
    <div className="boards-frame">
      <header className="boards-header">
        <div className="boards-heading">
          <h1>Mind maps</h1>
          <p className="muted">Arrange notes on a canvas; every node stays an ordinary document.</p>
          {mindMaps.length > 0 && <small className="boards-count" role="status">{mapCount(mindMaps.length)} in this vault{mapsStatus === 'loading' ? ' · Refreshing…' : ''}</small>}
        </div>
        <div className="spacer"/>
        {!writable && <span className="boards-readonly"><Icon name="eye" size={12}/>Read-only vault</span>}
        {writable && mindMaps.length > 0 && <button className="btn primary" onClick={create}><Icon name="plus" size={14}/>New mind map</button>}
      </header>
      {error && <div className="board-banner" role="alert">{error}</div>}
      {stale && <div className="board-banner warning" role="status"><span>{mapsError || 'Could not refresh the mind map list. Showing the maps already loaded.'}</span>{onRetryMaps && <button className="btn small" onClick={() => onRetryMaps()}>Retry</button>}</div>}
      <div className={'boards-body' + (empty || failed ? ' boards-body-centered' : '')}>
        {loading && <div className="boards-loading" role="status" aria-live="polite">
          <p className="boards-loading-note"><Icon name="graph" size={13}/>Loading mind maps…</p>
          <div className="boards-loading-grid" aria-hidden="true">{[0, 1, 2].map((i) => <div className="boards-skeleton" key={i}/>)}</div>
        </div>}
        {failed && <div className="boards-state" role="alert">
          <span className="boards-state-icon danger"><Icon name="help" size={26}/></span>
          <h2>Could not load mind maps</h2>
          <p>{mapsError || 'The mind map list could not be read from this vault.'}</p>
          {onRetryMaps && <button className="btn primary" onClick={() => onRetryMaps()}>Try again</button>}
        </div>}
        {empty && <div className="boards-state">
          <span className="boards-state-icon"><Icon name="graph" size={26}/></span>
          <h2>No mind maps yet</h2>
          <p>{writable
            ? 'Create a canvas, then add nodes or drag existing notes onto it. Nodes are ordinary Markdown documents in the map folder.'
            : 'No mind maps have been created in this vault. Ask its owner or an editor to create one.'}</p>
          {writable ? <button className="btn primary" onClick={create}><Icon name="plus" size={14}/>Create mind map</button> : null}
        </div>}
        {mindMaps.length > 0 && <div className="boards-grid" role="group" aria-label="Mind maps in this vault">{mindMaps.map((m) => <button type="button" className="board-summary" key={m.path} aria-label={`Open mind map ${m.title}`} aria-description={m.path} title={m.title + '\n' + m.path} onClick={() => onOpenMap(m.path)}>
          <header><strong>{m.title}</strong><small><Icon name="folder" size={12}/><span>{m.path}</span></small></header>
          <span className="board-summary-preview" aria-hidden="true">{Array.from({ length: Math.min(Math.max(m.nodes, 1), 5) }, (_, i) => <i key={i}/>)}</span>
          <small className="board-summary-count">{m.nodes} {m.nodes === 1 ? 'node' : 'nodes'} · {m.edges} {m.edges === 1 ? 'connector' : 'connectors'}</small>
          <footer><span className="btn small primary">Open canvas</span></footer>
        </button>)}</div>}
      </div>
    </div>
    {creating && <CreateMapDialog space={space} mindMaps={mindMaps} folders={folders} onClose={() => setCreating(false)} onCreated={(path) => { setCreating(false); onChanged(); onOpenMap(path); }} onError={setError}/>}
  </div>;
}

function CreateMapDialog({ space, mindMaps, folders, onClose, onCreated, onError }: {
  space: string; mindMaps: api.MindMapSummary[]; folders: string[];
  onClose: () => void; onCreated: (path: string) => void; onError: (message: string) => void;
}) {
  const [path, setPath] = useState('Mind maps/Untitled map');
  const [title, setTitle] = useState('');
  const [pathEdited,setPathEdited]=useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const idemRef = useRef<{ signature: string; key: string } | null>(null);

  const create = async () => {
    const problems: string[] = [];
    const pathErr = mindMapPathError(path, mindMaps.map((m) => m.path));
    if (pathErr) problems.push(pathErr);
    setErrors(problems);
    if (problems.length) return;
    setBusy(true);
    try {
      const cleanPath = path.trim().replace(/\/+$/, '');
      const body = { path: cleanPath, title: title.trim() || cleanPath.split('/').pop() || cleanPath };
      const signature = JSON.stringify(body);
      if (idemRef.current?.signature !== signature) idemRef.current = { signature, key: crypto.randomUUID() };
      const m = await api.createMindMap(space, body, idemRef.current.key);
      idemRef.current = null;
      onCreated(m.path);
    } catch (e) {
      if (api.ApiError.isConflict(e)) { onError(errMsg(e)); idemRef.current = null; }
      else setErrors([errMsg(e)]);
    } finally { setBusy(false); }
  };

  return <Dialog title="New mind map" close={()=>{if(!busy)onClose()}}>
    <p className="muted">Give your ideas a place to grow. Start with a blank canvas and add notes as you go.</p>
    <label className="field"><span>Mind map name</span><input data-autofocus value={title} onChange={e=>{const value=e.target.value;setTitle(value);if(!pathEdited)setPath('Mind maps/'+(value.replace(/[\\/:*?"<>|#^\[\]]/g,'').trim()||'Untitled map'))}} onKeyDown={e=>{if(e.key==='Enter'&&!busy)void create()}} placeholder="e.g. Product ideas"/></label>
    <details className="mindmap-location"><summary>Folder location</summary><label className="field"><span>Mind map folder</span>
      <input list="mindmap-folder-suggestions" value={path} onChange={e=>{setPathEdited(true);setPath(e.target.value)}}/>
      <datalist id="mindmap-folder-suggestions">{folders.map(f=><option key={f} value={f+'/map'}/>)}</datalist>
      <small className="muted">Contains the canvas configuration and new idea documents.</small>
    </label></details>
    {errors.length > 0 && <div className="board-banner" role="alert">{errors.map((e) => <p key={e}>{e}</p>)}</div>}
    <footer className="dialog-actions"><button className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy} onClick={() => void create()}>{busy ? 'Creating…' : 'Create mind map'}</button></footer>
  </Dialog>;
}
