import {expect,it} from 'vitest';
import {
  defaultFilters, filterListing, folderChoices, folderLabel, folderUpdatedMap,
  formatWhen, groupByFolder, hasActiveFilters, parentFolder,
} from './resourceListing';

it('derives parent folders and folder choices without the vault root',()=>{
  expect(parentFolder('Tasks/Project/board.md')).toBe('Tasks/Project');
  expect(parentFolder('Alpha')).toBe('');
  expect(folderChoices(['Tasks/a','Notes/b','Tasks/c','Root'])).toEqual(['Notes','Tasks']);
  expect(folderLabel('')).toBe('Vault root');
});

it('filters by query and folder selection',()=>{
  const items=[{path:'Tasks/Website launch',title:'Website launch'},{path:'Notes/Reading',title:'Reading queue'},{path:'Alpha',title:'Alpha'}];
  expect(filterListing(items,defaultFilters())).toHaveLength(3);
  expect(filterListing(items,{query:'read',folder:'*'}).map(i=>i.path)).toEqual(['Notes/Reading']);
  expect(filterListing(items,{query:'',folder:''}).map(i=>i.path)).toEqual(['Alpha']);
  expect(filterListing(items,{query:'',folder:'Tasks'}).map(i=>i.path)).toEqual(['Tasks/Website launch']);
  expect(hasActiveFilters(defaultFilters())).toBe(false);
  expect(hasActiveFilters({query:' ',folder:'*'})).toBe(false);
  expect(hasActiveFilters({query:'x',folder:'*'})).toBe(true);
});

it('groups by folder with the vault root first and stable item order',()=>{
  const groups=groupByFolder([{path:'Tasks/b',title:'b'},{path:'Root2',title:'r2'},{path:'Notes/a',title:'a'},{path:'Tasks/a',title:'a'}]);
  expect(groups.map(g=>g.folder)).toEqual(['','Notes','Tasks']);
  expect(groups[2].items.map(i=>i.path)).toEqual(['Tasks/a','Tasks/b']);
});

it('maps every ancestor folder to its newest document update',()=>{
  const map=folderUpdatedMap([
    {path:'Tasks/Project/a.md',updatedAt:'2026-01-02T00:00:00Z'},
    {path:'Tasks/Project/board.md',updatedAt:'2026-01-05T00:00:00Z'},
    {path:'Tasks/old.md',updatedAt:'2026-01-01T00:00:00Z',tombstoneAt:'2026-01-03T00:00:00Z'},
    {path:'Loose.md',updatedAt:'2026-01-04T00:00:00Z'},
  ]);
  expect(map.get('Tasks/Project')).toBe('2026-01-05T00:00:00Z');
  expect(map.get('Tasks')).toBe('2026-01-05T00:00:00Z');
  expect(map.get('')).toBe('2026-01-05T00:00:00Z');
});

it('formats modification times relative to now',()=>{
  const now=new Date('2026-01-10T12:00:00Z');
  expect(formatWhen('2026-01-10T09:00:00Z',now)).toContain('today at');
  expect(formatWhen('2026-01-09T09:00:00Z',now)).toContain('yesterday at');
  expect(formatWhen('2026-01-07T09:00:00Z',now)).toBe('3 days ago');
  expect(formatWhen('2025-12-20T09:00:00Z',now)).toContain('2025');
  expect(formatWhen('not a date',now)).toBe('');
});

it('compares activity timestamps chronologically and ignores invalid dates',()=>{
 const dates=folderUpdatedMap([{path:'Tasks/Board/a.md',updatedAt:'2026-10-20T12:00:00+02:00'},{path:'Tasks/Board/b.md',updatedAt:'2026-10-20T11:00:00Z'},{path:'Tasks/Board/c.md',updatedAt:'invalid'}]);
 expect(dates.get('Tasks/Board')).toBe('2026-10-20T11:00:00Z');
});
