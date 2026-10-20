// The board creation dialog, shared by the Boards list page and the file
// explorer's create menu so both flows stay identical (columns, templates,
// idempotent creation). `initialPath` lets a caller prefill the board folder.

import { useRef, useState } from 'react';
import * as api from '../api/client';
import { Dialog } from './Dialog';
import { Icon } from './Icon';
import { boardPathError, columnSlug, defaultBoardColumns, validateBoardColumns, type BoardColumnDraft } from '../lib/boards';

const errMsg = (e: unknown) => e instanceof Error ? e.message : 'Something went wrong.';

export function CreateBoardDialog({ space, boards, folders, initialPath = 'Tasks/New board', onClose, onCreated, onError }: {
  space: string; boards: api.BoardSummary[]; folders: string[]; initialPath?: string;
  onClose: () => void; onCreated: (path: string) => void; onError: (message: string) => void;
}) {
  const [path, setPath] = useState(initialPath);
  const [title, setTitle] = useState('');
  const [template, setTemplate] = useState('');
  const [columns, setColumns] = useState<BoardColumnDraft[]>(defaultBoardColumns());
  const [errors, setErrors] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const idemRef = useRef<{signature:string;key:string} | null>(null);

  const patch = (id: string, part: Partial<BoardColumnDraft>) => setColumns((old) => old.map((c) => c.id === id ? { ...c, ...part } : c));
  const addColumn = () => setColumns(old=>{const id=columnSlug('new column',new Set(old.flatMap(c=>[c.id,c.folder])));return [...old,{id,label:'New column',folder:id,completion:false,wipLimit:0}]});

  const create = async () => {
    if(busy)return;
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

  return <Dialog title="New board" close={()=>{if(!busy)onClose()}}>
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
   <footer className="dialog-actions"><button className="btn" disabled={busy} onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy} onClick={() => void create()}>{busy ? 'Creating…' : 'Create board'}</button></footer>
  </Dialog>;
}
