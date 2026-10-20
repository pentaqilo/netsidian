// BoardsPage: the "Boards" tool tab. Lists every folder-backed board in the
// vault grouped by its parent folder, with search and folder filtering,
// modification dates taken from real document metadata, and creation flows
// shared with the file explorer (CreateBoardDialog).

import { useMemo, useState } from 'react';
import * as api from '../api/client';
import { Icon } from '../components/Icon';
import { ResourceFilters } from '../components/ResourceFilters';
import { CreateBoardDialog } from '../components/CreateBoardDialog';
import {
  defaultFilters, filterListing, folderChoices, folderLabel, folderUpdatedMap,
  formatWhen, groupByFolder, hasActiveFilters, type ListFilters,
} from '../lib/resourceListing';

const columnCount = (n: number) => `${n} ${n === 1 ? 'column' : 'columns'}`;
const boardCount = (n: number) => `${n} ${n === 1 ? 'board' : 'boards'}`;

// How the board list itself arrived: still fetching, fetched, or failed. The
// page reads it to tell "loading", "genuinely empty" and "could not read"
// apart instead of showing a false empty state.
export type BoardsStatus = 'loading' | 'ready' | 'error';

export default function BoardsPage({ space, boards, folders, docs = [], writable, boardsStatus = 'ready', boardsError = '', onRetryBoards, onOpenBoard, onChanged }: {
  space: string;
  boards: api.BoardSummary[];
  folders: string[];
  // Loaded document metadata; only used to derive real "updated" dates for
  // board folders in one pass — the page never fetches per board.
  docs?: { path: string; updatedAt?: string | null; tombstoneAt?: string | null }[];
  writable: boolean;
  boardsStatus?: BoardsStatus;
  boardsError?: string;
  onRetryBoards?: () => void;
  onOpenBoard: (path: string) => void;
  onChanged: () => void;
}) {
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');
  const [filters, setFilters] = useState<ListFilters>(defaultFilters);

  const loading = boards.length === 0 && boardsStatus === 'loading';
  const failed = boards.length === 0 && boardsStatus === 'error';
  // A background refresh failed: keep the boards already on screen and say so
  // quietly instead of replacing them with an error or an empty state.
  const stale = boards.length > 0 && boardsStatus === 'error';
  const empty = boards.length === 0 && boardsStatus === 'ready';
  const create = () => { setError(''); setCreating(true); };

  const filtering = hasActiveFilters(filters);
  const visible = useMemo(() => filterListing(boards, filters), [boards, filters]);
  const groups = useMemo(() => groupByFolder(visible), [visible]);
  const updated = useMemo(() => folderUpdatedMap(docs), [docs]);
  const filterFolders = useMemo(() => folderChoices(boards.map((b) => b.path)), [boards]);
  // Filters only make sense once there is something to filter; empty, loading
  // and failed states keep the header free of extra controls.
  const showFilters = boards.length > 0;
  const filteredEmpty = showFilters && visible.length === 0;
  const clearFilters = () => setFilters(defaultFilters());

  return <div className="boards-page">
   <div className="boards-frame">
    <header className="boards-header">
     <div className="boards-heading">
      <h1>Boards</h1>
      <p className="muted">Organize notes and tasks by progress.</p>
      {boards.length > 0 && <small className="boards-count" role="status">{filtering ? `${visible.length} of ${boardCount(boards.length)} shown` : `${boardCount(boards.length)} in this vault`}{boardsStatus==='loading'?' · Refreshing…':''}</small>}
     </div>
     <div className="spacer"/>
     {!writable && <span className="boards-readonly"><Icon name="eye" size={12}/>Read-only vault</span>}
     {writable && boards.length > 0 && <button className="btn primary" onClick={create}><Icon name="plus" size={14}/>New board</button>}
    </header>
    {showFilters && <ResourceFilters query={filters.query} onQuery={(query) => setFilters((old) => ({ ...old, query }))} folder={filters.folder} onFolder={(folder) => setFilters((old) => ({ ...old, folder }))} folders={filterFolders} kind="board"/>}
    {error && <div className="board-banner" role="alert">{error}</div>}
    {stale && <div className="board-banner warning" role="status"><span>{boardsError || 'Could not refresh the board list. Showing the boards already loaded.'}</span>{onRetryBoards && <button className="btn small" onClick={() => onRetryBoards()}>Retry</button>}</div>}
    <div className={'boards-body' + (empty || failed || filteredEmpty ? ' boards-body-centered' : '')}>
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
     {filteredEmpty && <div className="boards-state">
      <span className="boards-state-icon"><Icon name="search" size={26}/></span>
      <h2>No boards match</h2>
      <p>No board matches the current search or folder filter. Adjust it or clear the filters to see every board in this vault.</p>
      <button className="btn" onClick={clearFilters}>Clear filters</button>
     </div>}
     {groups.map((group) => <section className="resource-group" key={group.folder || '/'} aria-label={`Boards in ${folderLabel(group.folder)}`}>
      <h2 className="resource-group-title"><Icon name="folder" size={13}/>{folderLabel(group.folder)}<small>{boardCount(group.items.length)}</small></h2>
      <div className="boards-grid maps-grid" role="group" aria-label={`Boards in ${folderLabel(group.folder)}`}>{group.items.map((b) => <button type="button" className="board-summary map-summary" key={b.path} aria-label={`Open board ${b.title}`} aria-description={b.path} title={b.title+'\n'+b.path} onClick={() => onOpenBoard(b.path)}>
        <header><strong>{b.title}</strong><small><Icon name="folder" size={12}/><span>{b.path}</span></small></header>
        <span className="map-summary-icon" aria-hidden="true"><Icon name="group" size={22}/></span>
        <small className="board-summary-count">{columnCount(b.columns)}{updated.has(b.path)&&<span className="board-summary-modified" title={new Date(updated.get(b.path)!).toLocaleString()}> · activity {formatWhen(updated.get(b.path)!)}</span>}</small>
       </button>)}</div>
     </section>)}
    </div>
   </div>
   {creating && <CreateBoardDialog space={space} boards={boards} folders={folders} onClose={() => setCreating(false)} onCreated={(path) => { setCreating(false); onChanged(); onOpenBoard(path); }} onError={setError}/>}
  </div>;
}
