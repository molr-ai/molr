// Cookies share a host across ports. Vite's dev/preview proxy gives each
// browser-facing port its own names, then restores Runtime's names upstream.
export const authCookieNames = [
  '__Host-molr_session', '__Host-molr_csrf', '__Host-molr_login',
  'molr_dev_session', 'molr_dev_csrf', 'molr_dev_login',
] as const;

export function scopedCookieName(name: string, port: string) {
  return `${name}_p${port}`;
}
