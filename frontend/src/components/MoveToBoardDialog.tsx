// MoveToBoardDialog: moves an existing document (note or memo) into a board
// column. The picker shows the board, the target column and the resulting
// vault path, so the folder mechanics stay visible here while the board's own
// drag UX hides them.

import { useEffect, useState } from 'react';
import * as api from '../api/client';
import { Dialog } from './Dialog';

const errMsg = (e: unknown) => e instanceof Error ? e.message : 'Something went wrong.';

export function MoveToBoardDialog({ space, doc, boards, onClose, onMoved }: {
  space: string;
  doc: { id: string; path: string; title: string };
  boards: api.BoardSummary[];
  onClose: () => void;
  onMoved: (boardPath: string, columnId: string) => Promise<void>;
}) {
  const [boardPath, setBoardPath] = useState(boards[0]?.path || '');
  const [board, setBoard] = useState<api.Board | null>(null);
  const [columnId, setColumnId] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!boardPath) { setBoard(null); return; }
    let active = true;setBoard(null);setColumnId('');setError('');
    api.getBoard(space, boardPath).then((b) => {
      if (!active) return;
      setBoard(b);
      setError('');
      const preferred = b.config.createDefaults?.column;
      setColumnId(preferred && b.columns.some((c) => c.id === preferred) ? preferred : b.columns[0]?.id || '');
    }).catch((e) => { if (active) setError(errMsg(e)); });
    return () => { active = false; };
  }, [space, boardPath]);

  const column = board?.columns.find((c) => c.id === columnId);
  const filename = doc.path.split('/').pop() || 'note.md';
  const preview = column ? `${boardPath}/${column.folder}/${filename}` : '';

  const move = async () => {
    if (!boardPath || !columnId || busy) return;
    setBusy(true);
    try {
      await onMoved(boardPath, columnId);
    } catch (e) {
      setError(errMsg(e));
    } finally { setBusy(false); }
  };

  return <Dialog title="Move to board" close={onClose}>
   <p className="muted">Moving <strong>{doc.title || doc.path}</strong> turns it into a card. Its Markdown content and links are preserved; only its folder changes.</p>
   {boards.length === 0
    ? <p>There are no boards in this vault yet. Open the <strong>Boards</strong> tab to create one.</p>
    : <>
     <label className="field"><span>Board</span>
      <select value={boardPath} onChange={(e) => setBoardPath(e.target.value)}>
       {boards.map((b) => <option key={b.path} value={b.path}>{b.title} ({b.path})</option>)}
      </select>
     </label>
     <label className="field"><span>Column</span>
      <select value={columnId} onChange={(e) => setColumnId(e.target.value)}>
       {(board?.columns || []).map((c) => <option key={c.id} value={c.id}>{c.label}{c.wipLimit && c.cards.length >= c.wipLimit ? ` (${c.cards.length}/${c.wipLimit})` : ''}</option>)}
      </select>
      {column?.wipLimit && column.cards.length >= column.wipLimit && <small className="muted">This column is at its soft WIP limit — the move is still allowed.</small>}
     </label>
     {preview && <p className="board-move-preview">New path: <code>{preview}</code></p>}
     {error && <div className="board-banner" role="alert">{error}</div>}
     <footer className="dialog-actions"><button className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !columnId || !board} onClick={() => void move()}>{busy ? 'Moving…' : 'Move to board'}</button></footer>
    </>}
  </Dialog>;
}
