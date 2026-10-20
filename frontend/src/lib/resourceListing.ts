// Shared listing logic for the Boards and Maps tool pages: grouping by parent
// folder, search/folder filtering, and modification dates derived from real
// document metadata. Everything here is pure so both pages reuse one
// implementation and it stays directly testable.

export interface ListingItem { path: string; title: string }

export const parentFolder = (path: string) => path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';

export const folderLabel = (folder: string) => folder || 'Vault root';

// Unique parent folders across the listing, sorted for stable filter options.
// The vault root ('') is offered separately by the folder select.
export function folderChoices(paths: string[]): string[] {
  const set = new Set(paths.map(parentFolder));
  set.delete('');
  return [...set].sort((a, b) => a.localeCompare(b));
}

// folder: '*' any folder, '' vault root only, otherwise an exact folder.
export interface ListFilters { query: string; folder: string }

export const defaultFilters = (): ListFilters => ({ query: '', folder: '*' });

export const hasActiveFilters = (filters: ListFilters) => filters.query.trim() !== '' || filters.folder !== '*';

export function filterListing<T extends ListingItem>(items: T[], filters: ListFilters): T[] {
  const q = filters.query.trim().toLocaleLowerCase();
  return items.filter(item => {
    if (filters.folder !== '*' && parentFolder(item.path) !== filters.folder) return false;
    if (!q) return true;
    return item.path.toLocaleLowerCase().includes(q) || item.title.toLocaleLowerCase().includes(q);
  });
}

export interface ListingGroup<T> { folder: string; items: T[] }

// Group by parent folder: vault root first, then folders alphabetically;
// items inside a group keep their path order.
export function groupByFolder<T extends ListingItem>(items: T[]): ListingGroup<T>[] {
  const map = new Map<string, T[]>();
  for (const item of items) {
    const folder = parentFolder(item.path);
    const list = map.get(folder);
    if (list) list.push(item); else map.set(folder, [item]);
  }
  return [...map.entries()]
    .sort((a, b) => a[0] === '' ? -1 : b[0] === '' ? 1 : a[0].localeCompare(b[0]))
    .map(([folder, group]) => ({ folder, items: group.sort((x, y) => x.path.localeCompare(y.path)) }));
}

// One pass over already-loaded document metadata builds a folder → newest
// updatedAt map covering every ancestor folder. Tiles read from this map, so
// listing dates never trigger per-item fetches (no N+1) and are never invented.
export function folderUpdatedMap(docs: { path: string; updatedAt?: string | null; tombstoneAt?: string | null }[]): Map<string, string> {
  const map = new Map<string, string>();
  for (const doc of docs) {
    if (!doc.updatedAt || doc.tombstoneAt || !Number.isFinite(Date.parse(doc.updatedAt))) continue;
    let folder = parentFolder(doc.path);
    for (;;) {
      const current = map.get(folder);
      if (!current || Date.parse(doc.updatedAt) > Date.parse(current)) map.set(folder, doc.updatedAt);
      if (!folder) break;
      folder = parentFolder(folder);
    }
  }
  return map;
}

// Human-friendly "when" for tiles: same-day and yesterday keep the clock time,
// the current week counts days, older entries fall back to a date.
export function formatWhen(iso: string, now = new Date()): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((dayStart(now) - dayStart(date)) / 86400000);
  const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  if (days === 0) return `today at ${time}`;
  if (days === 1) return `yesterday at ${time}`;
  if (days > 1 && days < 7) return `${days} days ago`;
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}
