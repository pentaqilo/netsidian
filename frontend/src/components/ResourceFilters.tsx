// Shared search + folder filter toolbar for the Boards and Maps list pages.
// Both pages drive it with the same ListFilters state from lib/resourceListing.

import { Icon, Tool } from './Icon';

export function ResourceFilters({ query, onQuery, folder, onFolder, folders, kind }: {
  query: string;
  onQuery: (value: string) => void;
  folder: string;
  onFolder: (value: string) => void;
  folders: string[];
  kind: 'board' | 'map';
}) {
  return <div className="resource-filters">
    <label className="resource-search">
      <Icon name="search" size={14}/>
      <input aria-label={`Search ${kind}s`} placeholder={`Search ${kind}s…`} value={query} onChange={e => onQuery(e.target.value)}/>
      {query && <Tool icon="close" label={`Clear ${kind} search`} onClick={() => onQuery('')}/>}
    </label>
    <label className="resource-folder">
      <Icon name="folder" size={14}/>
      <select aria-label={`Filter ${kind} folders`} value={folder} onChange={e => onFolder(e.target.value)}>
        <option value="*">All folders</option>
        <option value="">Vault root</option>
        {folders.map(f => <option key={f} value={f}>{f}</option>)}
      </select>
    </label>
  </div>;
}
