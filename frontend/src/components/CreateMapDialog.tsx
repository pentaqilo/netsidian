// The map creation dialog, shared by the Maps list page and the file
// explorer's create menu so both flows stay identical (name-driven folder,
// idempotent creation). `initialPath` lets a caller prefill the map folder.

import { useRef, useState } from 'react';
import * as api from '../api/client';
import { Dialog } from './Dialog';
import { mindMapPathError } from '../lib/mindmaps';

const errMsg = (e: unknown) => e instanceof Error ? e.message : 'Something went wrong.';

export function CreateMapDialog({ space, mindMaps, folders, initialPath = 'Mind maps/Untitled map', onClose, onCreated, onError }: {
  space: string; mindMaps: api.MindMapSummary[]; folders: string[]; initialPath?: string;
  onClose: () => void; onCreated: (path: string) => void; onError: (message: string) => void;
}) {
  const [path, setPath] = useState(initialPath);
  const [title, setTitle] = useState('');
  const [pathEdited, setPathEdited] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const idemRef = useRef<{ signature: string; key: string } | null>(null);
  // The folder the typed name lands in. A caller opening the dialog from the
  // vault root passes a bare name ("Untitled map"): that must stay at the
  // root, not be redirected into the default "Mind maps" folder.
  const baseDir = initialPath.includes('/') ? initialPath.slice(0, initialPath.lastIndexOf('/')) : '';

  const create = async () => {
    if(busy)return;
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

  return <Dialog title="New map" close={()=>{if(!busy)onClose()}}>
    <p className="muted">Give your ideas a place to grow. Start with a blank canvas and add notes as you go.</p>
    <label className="field"><span>Map name</span><input data-autofocus value={title} onChange={e=>{const value=e.target.value;setTitle(value);if(!pathEdited)setPath((baseDir?baseDir+'/':'')+(value.replace(/[\\/:*?"<>|#^\[\]]/g,'').trim()||'Untitled map'))}} onKeyDown={e=>{if(e.key==='Enter'&&!busy)void create()}} placeholder="e.g. Product ideas"/></label>
    <details className="mindmap-location"><summary>Folder location</summary><label className="field"><span>Map folder</span>
      <input list="mindmap-folder-suggestions" value={path} onChange={e=>{setPathEdited(true);setPath(e.target.value)}}/>
      <datalist id="mindmap-folder-suggestions">{folders.map(f=><option key={f} value={f+'/map'}/>)}</datalist>
      <small className="muted">Contains the canvas configuration and new idea documents.</small>
    </label></details>
    {errors.length > 0 && <div className="board-banner" role="alert">{errors.map((e) => <p key={e}>{e}</p>)}</div>}
    <footer className="dialog-actions"><button className="btn" disabled={busy} onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy} onClick={() => void create()}>{busy ? 'Creating…' : 'Create map'}</button></footer>
  </Dialog>;
}
