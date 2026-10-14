// Pure helpers for the folder-backed Kanban board UI. No React, no fetch —
// everything here is unit-testable and mirrors the backend's validation rules
// so the UI can refuse an invalid board edit before the server does.

export const BOARD_DRAG_KEY = 'application/netsidian-board';

export interface BoardDragPayload {
  cardId: string;
  docId: string;
  columnId: string;
  fromPath: string;
  revision: string;
}

export function parseBoardDrag(raw: string | null): BoardDragPayload | null {
  if (!raw || raw.length > 4096) return null;
  let value: unknown;
  try { value = JSON.parse(raw); } catch { return null; }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const p = value as Partial<Record<keyof BoardDragPayload, unknown>>;
  const str = (k: keyof BoardDragPayload): string | null => {
    const v = p[k];
    return typeof v === 'string' && v.length > 0 && v.length <= 1024 ? v : null;
  };
  const cardId = str('cardId'), docId = str('docId'), columnId = str('columnId'), fromPath = str('fromPath'), revision = str('revision');
  return cardId && docId && columnId && fromPath && revision ? { cardId, docId, columnId, fromPath, revision } : null;
}

// Mirrors the backend's single-folder-name rule for column folders.
export function columnFolderError(folder: string): string {
  if (!folder) return 'Column folder must not be empty.';
 if(folder.length>128||folder!==folder.normalize('NFC')||folder!==folder.trim()||/[\x00-\x1f\x7f%:*?"<>|]/.test(folder))return 'Column folder contains invalid characters or is too long.';
  if (folder.includes('/') || folder.includes('\\')) return 'Column folder must be a single folder name (no path separators).';
  if (folder === '.' || folder === '..') return 'Column folder must not traverse the vault.';
  if (folder.startsWith('.')) return 'Column folder must not be a hidden folder.';
  if (folder.toLowerCase() === 'assets') return '"assets" is reserved for non-task files.';
  if (folder.toLowerCase() === 'board') return '"board" is reserved for the board configuration file.';
  return '';
}

export interface BoardColumnDraft {
  id: string;
  label: string;
  folder: string;
  completion: boolean;
  wipLimit: number;
}

export function columnSlug(label: string, used: Set<string>): string {
  const base = label.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'column';
  let id = base, n = 2;
  while (used.has(id)) { id = `${base}-${n}`; n++; }
  return id;
}

export function validateBoardColumns(columns: BoardColumnDraft[], createDefaultsColumn = ''): string[] {
  const errors: string[] = [];
  if (!columns.length) return ['A board needs at least one column.'];
 if(columns.length>32)return ['A board supports at most 32 columns.'];
  const ids = new Set<string>();
  const folders = new Set<string>();
  for (const c of columns) {
    const name = c.id || c.label || c.folder;
    if(!/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(c.id))errors.push('Column id must use letters, numbers, underscores or hyphens.');
 if(!c.label.trim()||c.label.length>128)errors.push('Column label must contain 1 to 128 characters.');
    else if (ids.has(c.id)) errors.push(`Duplicate column id: ${c.id}`);
    ids.add(c.id);
    const folderErr = columnFolderError(c.folder);
    if (folderErr) errors.push(`Column "${name}": ${folderErr}`);
    const key = c.folder.normalize('NFD').toLowerCase();
    if (folders.has(key)) errors.push(`Duplicate column folder: ${c.folder}`);
    folders.add(key);
    if (!Number.isInteger(c.wipLimit) || c.wipLimit < 0 || c.wipLimit>10000) errors.push(`Column "${name}": WIP limit must be 0 or more.`);
  }
  if (createDefaultsColumn && !ids.has(createDefaultsColumn)) errors.push('Default column refers to a column that does not exist.');
  return errors;
}

// Codex's simplification: a column folder that still holds cards may not be
// removed or renamed — the cards must be moved to another column first.
export function populatedColumnRemovalErrors(
  existing: { id: string; label: string; folder: string; cards: number }[],
  next: BoardColumnDraft[],
): string[] {
  const kept = new Map(next.map(c=>[c.id,c.folder]));
  const out: string[] = [];
  for (const c of existing) {
    if (c.cards > 0 && kept.get(c.id)!==c.folder) {
      out.push(`Column "${c.label}" still holds ${c.cards} card${c.cards === 1 ? '' : 's'}. Move them to another column before removing or renaming its folder.`);
    }
  }
  return out;
}

export function boardPathError(path: string): string {
  const clean = path.trim().replace(/\/+$/, '');
  if (!clean) return 'Board folder must not be the vault root.';
 if(clean.length>1000||/[\x00-\x1f\x7f%\\:*?"<>|]/.test(clean))return 'Board folder contains invalid characters or is too long.';
  if (clean.split('/').some((s) => !s || s === '.' || s === '..')) return 'Board folder path must not contain "." or "..".';
  if (clean.split('/').some((s) => s.startsWith('.'))) return 'Board folders cannot live inside hidden folders.';
  return '';
}

// Compute the new card order for a within-column drop.
export function insertOrdered(ids: string[], movedId: string, beforeId: string | null, before = true): string[] {
  const rest = ids.filter((id) => id !== movedId);
  if (beforeId) {
    const i = rest.indexOf(beforeId);
    if (i >= 0) {
      const next = [...rest];
      next.splice(before ? i : i + 1, 0, movedId);
      return next;
    }
  }
  return [...rest, movedId];
}

// Compute the new card order for a keyboard (Alt+↑/↓) reorder step.
export function swapOrdered(ids: string[], movedId: string, dir: 1 | -1): string[] {
  const i = ids.indexOf(movedId);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= ids.length) return ids;
  const next = [...ids];
  next.splice(i, 1);
  next.splice(j, 0, movedId);
  return next;
}

export function randomIdemKey(prefix: string): string {
  const bytes = new Uint8Array(12);
  crypto.getRandomValues(bytes);
  return prefix + ':' + Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

export function defaultBoardColumns(): BoardColumnDraft[] {
  return [
    { id: 'todo', label: 'To do', folder: 'todo', completion: false, wipLimit: 0 },
    { id: 'in-progress', label: 'In progress', folder: 'in-progress', completion: false, wipLimit: 0 },
    { id: 'done', label: 'Done', folder: 'done', completion: true, wipLimit: 0 },
  ];
}
