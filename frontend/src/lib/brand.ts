import { isHex, type Theme } from './accent';

// Supplied n-14 dark/light SVG geometry: preserve the slanted stems while
// keeping the diagonal synchronized with the resolved application accent.
export const BRAND_BODY = `M30 58 86 26V195L30 228Z M172 79 228 46V197L172 230Z`;
export const BRAND_ACCENT = `M86 26 172 133V230L86 123Z`;

export const brandForeground = (theme: Theme) => theme === 'light' ? '#20201E' : '#FFFFFF';

export function brandSVG(theme: Theme, accent: string): string {
  // Serialize only validated colors; favicon markup never contains user text.
  const color = isHex(accent) ? accent : '#0561f5';
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256"><path fill="${brandForeground(theme)}" d="${BRAND_BODY}"/><path fill="${color}" d="${BRAND_ACCENT}"/></svg>`;
}

// A standalone favicon cannot inherit page CSS. Read the effective CSS accent
// after appearance updates, including contrast adjustment, theme and reset.
export function watchBrandFavicon(doc: Document = document): () => void {
  const root = doc.documentElement;
  const view = doc.defaultView;
  if (!view) return () => {};
  const icon = doc.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (!icon) return () => {};
  const original = icon.getAttribute('href');
  const update = () => {
    const styles = view.getComputedStyle(root);
    const theme: Theme = root.dataset.theme === 'light' ||
      (!root.dataset.theme && styles.colorScheme.startsWith('light')) ? 'light' : 'dark';
    const svg = brandSVG(theme, styles.getPropertyValue('--accent').trim());
    const href = 'data:image/svg+xml,' + encodeURIComponent(svg);
    if (icon.getAttribute('href') !== href) icon.setAttribute('href', href);
  };
  const observer = new view.MutationObserver(update);
  observer.observe(root, { attributes: true, attributeFilter: ['style', 'class', 'data-theme'] });
  update();
  return () => {
    observer.disconnect();
    if (original === null) icon.removeAttribute('href');
    else icon.setAttribute('href', original);
  };
}
