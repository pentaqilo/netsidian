import { isHex, type Theme } from './accent';

// Netsidian N mark: two foreground stems joined by an accent diagonal.
export const BRAND_BODY = `M48 48 H88 V208 H48 Z M168 48 H208 V208 H168 Z`;
export const BRAND_ACCENT = `M88 48 L168 152 L168 208 L88 104 Z`;

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
