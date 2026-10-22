// @vitest-environment jsdom
import darkLogo from '../../public/icons/n-14-dark.svg?raw';
import lightLogo from '../../public/icons/n-14-light.svg?raw';
import fallbackLogo from '../../public/icons/netsidian.svg?raw';
import { afterEach, expect, it } from 'vitest';
import { resolveAccent } from './accent';
import { BRAND_BODY,BRAND_ACCENT,brandSVG,watchBrandFavicon } from './brand';

let stop: (() => void) | undefined;
afterEach(() => {
  stop?.(); stop = undefined;
  document.head.innerHTML = '';
  document.documentElement.removeAttribute('style');
  delete document.documentElement.dataset.theme;
});

function colors(): string[] {
  const href = document.querySelector('link')!.getAttribute('href')!;
  const xml = decodeURIComponent(href.substring(href.indexOf(',') + 1));
  return [...new DOMParser().parseFromString(xml, 'image/svg+xml').querySelectorAll('path')]
    .map(path => path.getAttribute('fill')!);
}

it('keeps the favicon aligned with resolved custom, theme, preset and reset colors', async () => {
  document.head.innerHTML = '<link rel="icon" href="/icons/netsidian.svg">';
  const root = document.documentElement;
  root.dataset.theme = 'dark';
  root.style.setProperty('--accent', resolveAccent('dark', '#8ec07c', 'custom'));
  stop = watchBrandFavicon();
  expect(colors()).toEqual(['#FFFFFF', '#8ec07c']);

  root.dataset.theme = 'light';
  // A near-white custom color is adjusted by the application contrast gate.
  const adjusted = resolveAccent('light', '#ffffff', 'custom');
  root.style.setProperty('--accent', adjusted);
  await new Promise(resolve => setTimeout(resolve, 0));
  expect(colors()).toEqual(['#20201E', adjusted]);

  root.style.setProperty('--accent', resolveAccent('light', '', 'violet'));
  await new Promise(resolve => setTimeout(resolve, 0));
  expect(colors()).toEqual(['#20201E', '#6840bd']);
  root.style.setProperty('--accent', resolveAccent('light', '', 'default'));
  await new Promise(resolve => setTimeout(resolve, 0));
  expect(colors()).toEqual(['#20201E', '#70a4fb']);
});

it('restores the static fallback and stops updates when disposed', async () => {
  document.head.innerHTML = '<link rel="icon" href="/icons/netsidian.svg">';
  stop = watchBrandFavicon();
  stop(); stop = undefined;
  document.documentElement.style.setProperty('--accent', '#ff0000');
  await new Promise(resolve => setTimeout(resolve, 0));
  expect(document.querySelector('link')!.getAttribute('href')).toBe('/icons/netsidian.svg');
});


it('uses the supplied n-14 geometry consistently in both themes and the static fallback',()=>{
 for(const theme of ['dark','light'] as const){
  const source=theme==='dark'?darkLogo:lightLogo;
  const paths=[...new DOMParser().parseFromString(source,'image/svg+xml').querySelectorAll('path')];
  expect(BRAND_BODY).toBe(paths.filter(p=>p.getAttribute('fill')==='currentColor').map(p=>p.getAttribute('d')).join(' '));
  expect(BRAND_ACCENT).toBe(paths.find(p=>p.getAttribute('fill')!=='currentColor')?.getAttribute('d'));
  const generated=new DOMParser().parseFromString(brandSVG(theme,'#70a4fb'),'image/svg+xml');
  expect(generated.querySelector('path')?.getAttribute('fill')).toBe(theme==='dark'?'#FFFFFF':'#20201E');
 }
 const fallback=fallbackLogo;
 expect(fallback).toContain(BRAND_BODY);expect(fallback).toContain(BRAND_ACCENT);
});
