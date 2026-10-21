import type { Session } from 'electron';
import type { OAuthTransport } from './oauth.js';
import { normalizeURL } from './connections.js';

async function json(response: Response) {
  if (!response.ok || !response.headers.get('Content-Type')?.includes('application/json')) { await response.body?.cancel(); throw new Error('OAuth Runtime request failed'); }
  const reader = response.body?.getReader();
  if (!reader) throw new Error('OAuth Runtime response missing');
  const chunks: Uint8Array[] = []; let size = 0;
  try {
    for (;;) { const { done, value } = await reader.read(); if (done) break; size += value.length; if (size > 32 * 1024) throw new Error('OAuth Runtime response too large'); chunks.push(value); }
    return JSON.parse(Buffer.concat(chunks).toString('utf8')) as Record<string, unknown>;
  } finally { await reader.cancel().catch(() => {}); }
}

export function runtimeOAuthTransport(options: {
  url: string; allowHTTP: boolean; session: Session; current(): boolean;
  confirm(origin: string, signal: AbortSignal): Promise<boolean>; openExternal(url: string): Promise<void>;
}): OAuthTransport {
  // Explicitly opted-in private-network HTTP is supported, just like ordinary
  // Runtime traffic. Provider authorization and token endpoints still use TLS.
  const origin = normalizeURL(options.url, options.allowHTTP);
  let identity: string | undefined, csrf: string | undefined;
  async function check(signal: AbortSignal) {
    if (!options.current()) throw new Error('OAuth view changed');
    const response = await options.session.fetch(`${origin}/api/auth/me`, { redirect: 'error', signal, headers: { Origin: origin } });
    const cookieName = response.headers.get('X-Molr-CSRF-Cookie');
    const user = await json(response);
    if (typeof user.OwnerID !== 'string' || typeof user.SessionID !== 'string' || !user.SessionID || user.Kind !== 'owner' || user.TokenID) throw new Error('OAuth interactive login required');
    const next = JSON.stringify([user.OwnerID, user.SessionID]);
    if (identity && identity !== next) throw new Error('OAuth login changed');
    if (!identity) {
      if (!cookieName || !/^(?:__Host-molr_|molr_dev_|molr_lan_)csrf(?:_p[0-9]+)?$/.test(cookieName)) throw new Error('OAuth CSRF unavailable');
      const cookies = await options.session.cookies.get({ url: `${origin}/`, name: cookieName });
      if (cookies.length !== 1 || !cookies[0].value) throw new Error('OAuth CSRF unavailable');
      csrf = decodeURIComponent(cookies[0].value); identity = next;
    }
    if (!options.current()) throw new Error('OAuth view changed');
  }
  return {
    async request(path, body, signal) {
      if (!['/api/oauth/start', '/api/oauth/complete', '/api/oauth/cancel'].includes(path) && !/^\/api\/oauth\/attempts\/[A-Za-z0-9_-]{1,128}$/.test(path)) throw new Error('OAuth endpoint unavailable');
      const deadline = AbortSignal.any([AbortSignal.timeout(path === '/api/oauth/complete' ? 90_000 : 30_000), ...(signal ? [signal] : [])]);
      await check(deadline);
      const response = await options.session.fetch(origin + path, { method: body === undefined ? 'GET' : 'POST', redirect: 'error', signal: deadline, headers: { Origin: origin, 'Content-Type': 'application/json', 'X-CSRF-Token': csrf! }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
      const result = await json(response);
      if (!options.current()) throw new Error('OAuth view changed');
      return result;
    },
    confirm: options.confirm,
    async openExternal(url) { if (!options.current()) throw new Error('OAuth view changed'); await options.openExternal(url); },
  };
}
