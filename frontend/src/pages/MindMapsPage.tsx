// MindMapsPage: the "Maps" tool tab. Lists every folder-backed mind map in
// the vault grouped by its parent folder, with search and folder filtering,
// modification dates taken from real document metadata, and a creation flow
// shared with the file explorer (CreateMapDialog).

import { useMemo, useState } from 'react';
import * as api from '../api/client';
import { Icon } from '../components/Icon';
import { ResourceFilters } from '../components/ResourceFilters';
import { CreateMapDialog } from '../components/CreateMapDialog';
import {
  defaultFilters, filterListing, folderChoices, folderLabel, folderUpdatedMap,
  formatWhen, groupByFolder, hasActiveFilters, type ListFilters,
} from '../lib/resourceListing';

const mapCount = (n: number) => `${n} ${n === 1 ? 'map' : 'maps'}`;

export type MindMapsStatus = 'loading' | 'ready' | 'error';

export default function MindMapsPage({ space, mindMaps, folders, docs = [], writable, mapsStatus = 'ready', mapsError = '', onRetryMaps, onOpenMap, onChanged }: {
  space: string;
  mindMaps: api.MindMapSummary[];
  folders: string[];
  // Loaded document metadata; only used to derive real "updated" dates for
  // map folders in one pass — the page never fetches per map.
  docs?: { path: string; updatedAt?: string | null; tombstoneAt?: string | null }[];
  writable: boolean;
  mapsStatus?: MindMapsStatus;
  mapsError?: string;
  onRetryMaps?: () => void;
  onOpenMap: (path: string) => void;
  onChanged: () => void;
}) {
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');
  const [filters, setFilters] = useState<ListFilters>(defaultFilters);

  const loading = mindMaps.length === 0 && mapsStatus === 'loading';
  const failed = mindMaps.length === 0 && mapsStatus === 'error';
  const stale = mindMaps.length > 0 && mapsStatus === 'error';
  const empty = mindMaps.length === 0 && mapsStatus === 'ready';
  const create = () => { setError(''); setCreating(true); };

  const filtering = hasActiveFilters(filters);
  const visible = useMemo(() => filterListing(mindMaps, filters), [mindMaps, filters]);
  const groups = useMemo(() => groupByFolder(visible), [visible]);
  const updated = useMemo(() => folderUpdatedMap(docs), [docs]);
  const filterFolders = useMemo(() => folderChoices(mindMaps.map((m) => m.path)), [mindMaps]);
  // Filters only make sense once there is something to filter; empty, loading
  // and failed states keep the header free of extra controls.
  const showFilters = mindMaps.length > 0;
  const filteredEmpty = showFilters && visible.length === 0;
  const clearFilters = () => setFilters(defaultFilters());

  return <div className="boards-page">
    <div className="boards-frame">
      <header className="boards-header">
        <div className="boards-heading">
          <h1>Maps</h1>
          <p className="muted">Arrange notes on a canvas; every node stays an ordinary document.</p>
          {mindMaps.length > 0 && <small className="boards-count" role="status">{filtering ? `${visible.length} of ${mapCount(mindMaps.length)} shown` : `${mapCount(mindMaps.length)} in this vault`}{mapsStatus === 'loading' ? ' · Refreshing…' : ''}</small>}
        </div>
        <div className="spacer"/>
        {!writable && <span className="boards-readonly"><Icon name="eye" size={12}/>Read-only vault</span>}
        {writable && mindMaps.length > 0 && <button className="btn primary" onClick={create}><Icon name="plus" size={14}/>New map</button>}
      </header>
      {showFilters && <ResourceFilters query={filters.query} onQuery={(query) => setFilters((old) => ({ ...old, query }))} folder={filters.folder} onFolder={(folder) => setFilters((old) => ({ ...old, folder }))} folders={filterFolders} kind="map"/>}
      {error && <div className="board-banner" role="alert">{error}</div>}
      {stale && <div className="board-banner warning" role="status"><span>{mapsError || 'Could not refresh the map list. Showing the maps already loaded.'}</span>{onRetryMaps && <button className="btn small" onClick={() => onRetryMaps()}>Retry</button>}</div>}
      <div className={'boards-body' + (empty || failed || filteredEmpty ? ' boards-body-centered' : '')}>
        {loading && <div className="boards-loading" role="status" aria-live="polite">
          <p className="boards-loading-note"><Icon name="graph" size={13}/>Loading maps…</p>
          <div className="boards-loading-grid" aria-hidden="true">{[0, 1, 2].map((i) => <div className="boards-skeleton" key={i}/>)}</div>
        </div>}
        {failed && <div className="boards-state" role="alert">
          <span className="boards-state-icon danger"><Icon name="help" size={26}/></span>
          <h2>Could not load maps</h2>
          <p>{mapsError || 'The map list could not be read from this vault.'}</p>
          {onRetryMaps && <button className="btn primary" onClick={() => onRetryMaps()}>Try again</button>}
        </div>}
        {empty && <div className="boards-state">
          <span className="boards-state-icon"><Icon name="mindmap" size={26}/></span>
          <h2>No maps yet</h2>
          <p>{writable
            ? 'Create a canvas, then add nodes or drag existing notes onto it. Nodes are ordinary Markdown documents in the map folder.'
            : 'No maps have been created in this vault. Ask its owner or an editor to create one.'}</p>
          {writable ? <button className="btn primary" onClick={create}><Icon name="plus" size={14}/>Create map</button> : null}
        </div>}
        {filteredEmpty && <div className="boards-state">
          <span className="boards-state-icon"><Icon name="search" size={26}/></span>
          <h2>No maps match</h2>
          <p>No map matches the current search or folder filter. Adjust it or clear the filters to see every map in this vault.</p>
          <button className="btn" onClick={clearFilters}>Clear filters</button>
        </div>}
        {groups.map((group) => <section className="resource-group" key={group.folder || '/'} aria-label={`Maps in ${folderLabel(group.folder)}`}>
          <h2 className="resource-group-title"><Icon name="folder" size={13}/>{folderLabel(group.folder)}<small>{mapCount(group.items.length)}</small></h2>
          <div className="boards-grid maps-grid" role="group" aria-label={`Maps in ${folderLabel(group.folder)}`}>{group.items.map((m) => <button type="button" className="board-summary map-summary" key={m.path} aria-label={`Open map ${m.title}`} aria-description={m.path} title={m.title + '\n' + m.path} onClick={() => onOpenMap(m.path)}>
            <header><strong>{m.title}</strong><small><Icon name="folder" size={12}/><span>{m.path}</span></small></header>
            <span className="map-summary-icon" aria-hidden="true"><Icon name="mindmap" size={22}/></span>
            <small className="board-summary-count">{m.nodes} {m.nodes === 1 ? 'node' : 'nodes'} · {m.edges} {m.edges === 1 ? 'connector' : 'connectors'}{updated.has(m.path)&&<span className="board-summary-modified" title={new Date(updated.get(m.path)!).toLocaleString()}> · activity {formatWhen(updated.get(m.path)!)}</span>}</small>
          </button>)}</div>
        </section>)}
      </div>
    </div>
    {creating && <CreateMapDialog space={space} mindMaps={mindMaps} folders={folders} onClose={() => setCreating(false)} onCreated={(path) => { setCreating(false); onChanged(); onOpenMap(path); }} onError={setError}/>}
  </div>;
}
