import { BRAND_ACCENT, BRAND_BODY } from '../lib/brand';

export function AppLogo({ size = 28, className = '', label }: { size?: number; className?: string; label?: string }) {
  return <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256"
    width={size} height={size} className={'app-icon app-logo ' + className}
    role={label ? 'img' : undefined} aria-label={label} aria-hidden={label ? undefined : true}
    focusable="false">
    <path fill="currentColor" d={BRAND_BODY}/>
    <path fill="var(--accent, #0561f5)" d={BRAND_ACCENT}/>
  </svg>;
}
