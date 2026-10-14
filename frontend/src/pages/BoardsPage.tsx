// BoardsPage: the "Boards" tool tab. Lists every folder-backed board in the
// vault, creates new ones (blank with editable columns, or copied from an
// existing board as a template), and opens them as workspace tabs.

import { useRef, useState } from 'react';
import * as api from '../api/client';
import { Dialog } from '../components/Dialog';
import { Icon } from '../components/Icon';
import { boardPathError, columnSlug, defaultBoardColumns, validateBoardColumns, type BoardColumnDraft } from '../lib/boards';

const errMsg = (e: unknown) => e instanceof Error ? e.message : 'Something went wrong.';
const columnCount = (n: number) => `${n} ${n === 1 ? 'column' : 'columns'}`;
const boardCount = (n: number) => `${n} ${n === 1 ? 'board' : 'boards'}`;

// How the board list itself arrived: still fetching, fetched, or failed. The
// page reads it to tell "loading", "genuinely empty" and "could not read"
// apart instead of showing a false empty state.
export type BoardsStatus = 'loading' | 'ready' | 'error';

export default function BoardsPage({ space, boards, folders, writable, boardsStatus = 'ready', boardsError = '', onRetryBoards, onOpenBoard, onChanged }: {
  space: string;
  boards: api.BoardSummary[];
  folders: string[];
  writable: boolean;
  boardsStatus?: BoardsStatus;
  boardsError?: string;
  onRetryBoards?: () => void;
  onOpenBoard: (path: string) => void;
  onChanged: () => void;
}) {
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');

  const loading = boards.length === 0 && boardsStatus === 'loading';
  const failed = boards.length === 0 && boardsStatus === 'error';
  // A background refresh failed: keep the boards already on screen and say so
  // quietly instead of replacing them with an error or an empty state.
  const stale = boards.length > 0 && boardsStatus === 'error';
  const empty = boards.length === 0 && boardsStatus === 'ready';
  const create = () => { setError(''); setCreating(true); };

  return <div className="boards-page">
   <div className="boards-frame">
    <header className="boards-header">
     <div className="boards-heading">
      <h1>Boards</h1>
      <p className="muted">Organize notes and tasks by progress.</p>
      {boards.length > 0 && <small className="boards-count" role="status">{boardCount(boards.length)} in this vault{boardsStatus==='loading'?' · Refreshing…':''}</small>}
     </div>
     <div className="spacer"/>
     {!writable && <span className="boards-readonly"><Icon name="eye" size={12}/>Read-only vault</span>}
     {writable && boards.length > 0 && <button className="btn primary" onClick={create}><Icon name="plus" size={14}/>New board</button>}
    </header>
    {error && <div className="board-banner" role="alert">{error}</div>}
    {stale && <div className="board-banner warning" role="status"><span>{boardsError || 'Could not refresh the board list. Showing the boards already loaded.'}</span>{onRetryBoards && <button className="btn small" onClick={() => onRetryBoards()}>Retry</button>}</div>}
    <div className={'boards-body' + (empty || failed ? ' boards-body-centered' : '')}>
     {loading && <div className="boards-loading" role="status" aria-live="polite">
      <p className="boards-loading-note"><Icon name="group" size={13}/>Loading boards…</p>
      <div className="boards-loading-grid" aria-hidden="true">{[0, 1, 2].map((i) => <div className="boards-skeleton" key={i}/>)}</div>
     </div>}
     {failed && <div className="boards-state" role="alert">
      <span className="boards-state-icon danger"><Icon name="help" size={26}/></span>
      <h2>Could not load boards</h2>
      <p>{boardsError || 'The board list could not be read from this vault.'}</p>
      {onRetryBoards && <button className="btn primary" onClick={() => onRetryBoards()}>Try again</button>}
     </div>}
     {empty && <div className="boards-state">
      <span className="boards-state-icon"><Icon name="group" size={26}/></span>
      <h2>No boards yet</h2>
      <p>{writable
        ? 'Create a board to organize tasks into To do, In progress, and Done. Add existing notes or create new tasks as you go.'
        : 'No boards have been created in this vault. Ask its owner or an editor to create one.'}</p>
      {writable
        ? <button className="btn primary" onClick={create}><Icon name="plus" size={14}/>Create board</button>
        : null}
     </div>}
     {boards.length > 0 && <div className="boards-grid" role="group" aria-label="Boards in this vault">{boards.map((b) => <button type="button" className="board-summary" key={b.path} aria-label={`Open board ${b.title}`} aria-description={b.path} title={b.title+'\n'+b.path} onClick={() => onOpenBoard(b.path)}>
       <header><strong>{b.title}</strong><small><Icon name="folder" size={12}/><span>{b.path}</span></small></header>
       <span className="board-summary-preview" aria-hidden="true">{Array.from({ length: Math.min(Math.max(b.columns, 1), 5) }, (_, i) => <i key={i}/>)}</span>
       <small className="board-summary-count">{columnCount(b.columns)}</small>
       <footer><span className="btn small primary">Open board</span></footer>
      </button>)}</div>}
    </div>
   </div>
   {creating && <CreateBoardDialog space={space} boards={boards} folders={folders} onClose={() => setCreating(false)} onCreated={(path) => { setCreating(false); onChanged(); onOpenBoard(path); }} onError={setError}/>}
  </div>;
}

function CreateBoardDialog({ space, boards, folders, onClose, onCreated, onError }: {
  space: string; boards: api.BoardSummary[]; folders: string[];
  onClose: () => void; onCreated: (path: string) => void; onError: (message: string) => void;
}) {
  const [path, setPath] = useState('Tasks/New board');
  const [title, setTitle] = useState('');
  const [template, setTemplate] = useState('');
  const [columns, setColumns] = useState<BoardColumnDraft[]>(defaultBoardColumns());
  const [errors, setErrors] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const idemRef = useRef<{signature:string;key:string} | null>(null);

  const patch = (id: string, part: Partial<BoardColumnDraft>) => setColumns((old) => old.map((c) => c.id === id ? { ...c, ...part } : c));
  const addColumn = () => setColumns(old=>{const id=columnSlug('new column',new Set(old.flatMap(c=>[c.id,c.folder])));return [...old,{id,label:'New column',folder:id,completion:false,wipLimit:0}]});

  const create = async () => {
    const problems: string[] = [];
    const pathErr = boardPathError(path);
    if (pathErr) problems.push(pathErr);
    if (!template) problems.push(...validateBoardColumns(columns));
    setErrors(problems);
    if (problems.length) return;

    setBusy(true);
    try {
      const cleanPath = path.trim().replace(/\/+$/, '');
      const body = {
        path: cleanPath,
        title: title.trim() || cleanPath.split('/').pop() || cleanPath,
        ...(template ? { template } : { columns: columns.map((c) => ({ id: c.id, label: c.label.trim() || c.id, folder: c.folder.trim(), completion: c.completion, wipLimit: c.wipLimit })) }),
      };
 const signature=JSON.stringify(body);if(idemRef.current?.signature!==signature)idemRef.current={signature,key:crypto.randomUUID()};
 const board=await api.createBoard(space,body,idemRef.current.key);
      idemRef.current = null;
      onCreated(board.path);
    } catch (e) {
      if (api.ApiError.isConflict(e)) { onError(errMsg(e)); idemRef.current = null; }
      else setErrors([errMsg(e)]);
    } finally { setBusy(false); }
  };

  return <Dialog title="New board" close={onClose}>
   <label className="field"><span>Board folder</span>
    <input data-autofocus list="board-folder-suggestions" value={path} onChange={(e) => setPath(e.target.value)} placeholder="Tasks/Project"/>
    <datalist id="board-folder-suggestions">{folders.map((f) => <option key={f} value={f + '/board'}/>)}</datalist>
    <small className="muted">A new folder (or an existing one) that will hold the columns. Not the vault root.</small>
   </label>
   <label className="field"><span>Board title</span><input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Leave empty to use the folder name"/></label>
   <label className="field"><span>Copy columns from an existing board (template)</span>
    <select value={template} onChange={(e) => setTemplate(e.target.value)}>
     <option value="">— blank board —</option>
     {boards.map((b) => <option key={b.path} value={b.path}>{b.title} ({b.path})</option>)}
    </select>
   </label>
   {!template && <div className="board-settings-columns">
    <strong>Starting columns</strong>
    {columns.map((c) => <div className="board-column-row" key={c.id}>
     <input aria-label="Column label" value={c.label} onChange={(e) => patch(c.id, { label: e.target.value })}/>
     <input aria-label="Column folder" value={c.folder} onChange={(e) => patch(c.id, { folder: e.target.value })}/>
     <label className="board-completion"><input type="checkbox" checked={c.completion} onChange={(e) => patch(c.id, { completion: e.target.checked })}/>Done</label>
     <input type="number" min={0} aria-label="Soft WIP limit" title="Soft WIP limit (0 = none)" value={c.wipLimit} onChange={(e) => patch(c.id, { wipLimit: Number(e.target.value) })}/>
     <span/>
     <button className="btn small" aria-label={'Remove column ' + c.label} disabled={columns.length <= 1} onClick={() => setColumns((old) => old.filter((x) => x.id !== c.id))}><Icon name="trash" size={13}/></button>
    </div>)}
    <button className="btn small" onClick={addColumn}><Icon name="plus" size={13}/>Add column</button>
   </div>}
   {errors.length > 0 && <div className="board-banner" role="alert">{errors.map((e) => <p key={e}>{e}</p>)}</div>}
   <footer className="dialog-actions"><button className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy} onClick={() => void create()}>{busy ? 'Creating…' : 'Create board'}</button></footer>
  </Dialog>;
}
