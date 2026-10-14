// P3-A01: document accent resolution with WCAG relative-luminance contrast gate.
export type AccentPreset = 'default' | 'blue' | 'violet' | 'neutral' | 'custom';
export type Theme = 'dark' | 'light';

const VIOLET: Record<Theme, string> = { dark: '#b7a0ff', light: '#6840bd' };
const NEUTRAL: Record<Theme, string> = { dark: '#dadada', light: '#414141' };
const BG: Record<Theme, string> = { dark: '#1e1e1e', light: '#ffffff' };
export const MIN_CONTRAST = 4.5;

export const isHex = (s: string) => /^#[\da-f]{6}$/i.test(s);

export function relLuminance(hex: string): number {
  const c = hex
    .slice(1)
    .match(/../g)!
    .map((v) => parseInt(v, 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)));
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
}

export function contrastRatio(a: string, b: string): number {
  const l1 = relLuminance(a);
  const l2 = relLuminance(b);
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
}

function mix(hex: string, other: string, t: number): string {
  const a = hex.slice(1).match(/../g)!.map((v) => parseInt(v, 16));
  const b = other.slice(1).match(/../g)!.map((v) => parseInt(v, 16));
  return '#' + a.map((v, i) => Math.round(v + (b[i] - v) * t).toString(16).padStart(2, '0')).join('');
}

// Legacy stored preferences have no preset: a non-empty custom hex stays custom, an absent value means default.
export function effectivePreset(accent: string, preset?: AccentPreset): AccentPreset {
  return preset ?? (isHex(accent) ? 'custom' : 'default');
}

export function resolveAccent(theme: Theme, accent: string, preset?: AccentPreset): string {
  const p = effectivePreset(accent, preset);
  if (p === 'default' || p === 'blue') return '#0561f5';
  if (p === 'neutral') return NEUTRAL[theme];
  const base = p === 'custom' && isHex(accent) ? accent : VIOLET[theme];
  if (contrastRatio(base, BG[theme]) >= MIN_CONTRAST) return base;
  // Move a low-contrast custom color toward the theme foreground until it is readable.
  const toward = theme === 'dark' ? '#ffffff' : '#000000';
  for (let t = 0.05; t <= 1; t += 0.05) {
    const next = mix(base, toward, t);
    if (contrastRatio(next, BG[theme]) >= MIN_CONTRAST) return next;
  }
  return theme === 'dark' ? '#ffffff' : '#000000';
}

// Move a low-contrast color toward the theme foreground until it clears the gate
// against the theme background. Shared by the document accent and the memo accent.
function readableAgainst(base: string, theme: Theme): string {
  if (contrastRatio(base, BG[theme]) >= MIN_CONTRAST) return base;
  const toward = theme === 'dark' ? '#ffffff' : '#000000';
  for (let t = 0.05; t <= 1; t += 0.05) {
    const next = mix(base, toward, t);
    if (contrastRatio(next, BG[theme]) >= MIN_CONTRAST) return next;
  }
  return theme === 'dark' ? '#ffffff' : '#000000';
}

// Memos have their own accent, independent of the document accent. Built-in hues
// are the historical greens; a user pick wins and Reset returns to these.
export const MEMO_DEFAULT: Record<Theme, string> = { dark: '#a6c6ab', light: '#41714e' };

// The memo accent is used both as foreground text (on the page background) and as
// a fill (buttons/badges). Gate it for readability against the page background,
// then pick the on-accent text color for contrast against the accent itself.
export function resolveMemoAccent(theme: Theme, color: string): { accent: string; text: string } {
  const base = isHex(color) ? color : MEMO_DEFAULT[theme];
  const accent = readableAgainst(base, theme);
  const text = contrastRatio(accent, '#ffffff') >= contrastRatio(accent, '#111111') ? '#ffffff' : '#111111';
  return { accent, text };
}
