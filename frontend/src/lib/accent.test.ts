import { describe, expect, it } from 'vitest';
import { contrastRatio, effectivePreset, isHex, relLuminance, resolveAccent, resolveMemoAccent, MEMO_DEFAULT, MIN_CONTRAST } from './accent';

describe('accent tokens (P3-A01)', () => {
  it('uses WCAG relative luminance, not naive luma', () => {
    expect(relLuminance('#ffffff')).toBeCloseTo(1, 5);
    expect(relLuminance('#000000')).toBeCloseTo(0, 5);
    expect(contrastRatio('#ffffff', '#000000')).toBeCloseTo(21, 1);
  });
  it('defaults to the exact blue brand color in both themes', () => {
    const dark = resolveAccent('dark', '');
    const light = resolveAccent('light', '');
    expect(dark).toBe('#0561f5');
    expect(light).toBe('#0561f5');
    expect(contrastRatio(light, '#ffffff')).toBeGreaterThanOrEqual(MIN_CONTRAST);
  });
  it('keeps violet available as a named preset',()=>{expect(resolveAccent('dark','','violet')).toBe('#b7a0ff');expect(resolveAccent('light','','violet')).toBe('#6840bd')});
  it('neutral is an explicit gray preset distinct from the default', () => {
    expect(resolveAccent('dark', '', 'neutral')).toBe('#dadada');
    expect(resolveAccent('light', '', 'neutral')).toBe('#414141');
    expect(resolveAccent('dark', '', 'neutral')).not.toBe(resolveAccent('dark', '', 'default'));
  });
  it('keeps valid custom colors that already pass the gate', () => {
    expect(resolveAccent('dark', '#8ec07c', 'custom')).toBe('#8ec07c');
    expect(resolveAccent('dark', '#8ec07c')).toBe('#8ec07c'); // legacy prefs stay custom
  });
  it('adjusts low-contrast custom colors toward the theme foreground', () => {
    const dark = resolveAccent('dark', '#ff0000', 'custom');
    expect(dark).not.toBe('#ff0000');
    expect(contrastRatio(dark, '#1e1e1e')).toBeGreaterThanOrEqual(MIN_CONTRAST);
    const light = resolveAccent('light', '#3a86ff', 'custom');
    expect(contrastRatio(light, '#ffffff')).toBeGreaterThanOrEqual(MIN_CONTRAST);
  });
  it('reset means the blue default, not the old gray fallback', () => {
    expect(effectivePreset('', 'default')).toBe('default');
    expect(resolveAccent('dark', '', 'default')).not.toBe('#dadada');
  });
  it('preserves non-empty custom hex values', () => {
    expect(isHex('#6840BD')).toBe(true);
    expect(isHex('6840bd')).toBe(false);
    expect(effectivePreset('#123456')).toBe('custom');
  });
});

describe('memo accent (independent of document accent)', () => {
  it('falls back to the built-in green when no color is set', () => {
    expect(resolveMemoAccent('dark', '').accent).toBe(MEMO_DEFAULT.dark);
    expect(resolveMemoAccent('light', '').accent).toBe(MEMO_DEFAULT.light);
    expect(resolveMemoAccent('dark', 'notahex').accent).toBe(MEMO_DEFAULT.dark);
  });
  it('gates a custom color for readability against the page background', () => {
    const dark = resolveMemoAccent('dark', '#ff0000');
    expect(dark.accent).not.toBe('#ff0000');
    expect(contrastRatio(dark.accent, '#1e1e1e')).toBeGreaterThanOrEqual(MIN_CONTRAST);
    const light = resolveMemoAccent('light', '#f0f0f0');
    expect(contrastRatio(light.accent, '#ffffff')).toBeGreaterThanOrEqual(MIN_CONTRAST);
  });
  it('picks the on-accent text color for contrast', () => {
    expect(resolveMemoAccent('dark', '#a6c6ab').text).toBe('#111111'); // light green -> dark text
    expect(resolveMemoAccent('light', '#41714e').text).toBe('#ffffff'); // deep green -> white text
  });
});
