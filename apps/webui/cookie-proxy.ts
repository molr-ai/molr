import { authCookieNames, scopedCookieName } from './src/api/cookie-names.ts';

export function runtimeCookies(header: string | undefined, port: string, runtimePort = port) {
  const cookies = (header ?? '').split(';').map(cookie => cookie.trim());
  // Do not forward legacy credentials or another WebUI's credentials. In
  // particular, never fall back to unscoped cookies during migration.
  return [...authCookieNames, ...["session", "csrf", "login"].map(kind => `molr_lan_${kind}`)].flatMap(name => {
    const prefix = `${scopedCookieName(name, port)}=`;
    const cookie = cookies.find(value => value.startsWith(prefix));
    return cookie ? [`${name.startsWith("molr_lan_") ? scopedCookieName(name, runtimePort) : name}=${cookie.slice(prefix.length)}`] : [];
  }).join('; ');
}

export function browserCookie(cookie: string, port: string) {
  const separator = cookie.indexOf('=');
  const name = cookie.slice(0, separator);
  if (/^molr_lan_(session|csrf|login)_p[0-9]+$/.test(name)) return name.replace(/_p[0-9]+$/, `_p${port}`) + cookie.slice(separator);
  if (!authCookieNames.some(known => known === name)) return cookie;
  // Preserve HttpOnly, Secure, SameSite, Path and expiry, including deletions.
  return scopedCookieName(name, port) + cookie.slice(separator);
}
