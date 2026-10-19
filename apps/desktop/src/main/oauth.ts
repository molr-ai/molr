import { createServer } from 'node:http';
import { timingSafeEqual } from 'node:crypto';
import type { AppOAuthResult } from '@molr/ui-host/app';

const identifier = /^[A-Za-z0-9_-]{1,128}$/;
export const oauthLifetime = 5 * 60_000;
type Callback = { State: string; Code: string; ClientID: string; Issuer: string; Error: string };
type Started = { ID: string; AuthorizationURL: string; State: string; ExpiresAt: string };
type Receipt = { ID: string; Status: string; ConnectionID?: string };
export type OAuthTransport = {
  // Only these fixed Runtime endpoints are called. The transport captures the
  // original connection/session and rejects requests after it changes.
  request(path: string, body?: unknown, signal?: AbortSignal): Promise<unknown>;
  confirm(authorizationOrigin: string, signal: AbortSignal): Promise<boolean>;
  openExternal(url: string): Promise<void>;
};
function equal(a: string, b: string) { return a.length === b.length && timingSafeEqual(Buffer.from(a), Buffer.from(b)); }

/** An ephemeral HTTP listener, never a general localhost proxy or file server. */
export async function listenCallback(signal: AbortSignal) {
  let expectedState = '', consumed = false;
  let resolve!: (value: Callback) => void;
  let reject!: (error: Error) => void;
  const response = new Promise<Callback>((yes, no) => { resolve = yes; reject = no; });
  // The listener can be cancelled while Runtime is still preparing the URL.
  void response.catch(() => {});
  const server = createServer({ maxHeaderSize: 12 * 1024, requestTimeout: 5000, headersTimeout: 5000 }, (req, res) => {
    res.setHeader('Cache-Control', 'no-store');
    res.setHeader('Content-Type', 'text/plain; charset=utf-8');
    res.setHeader('Content-Security-Policy', "default-src 'none'; frame-ancestors 'none'");
    res.setHeader('Referrer-Policy', 'no-referrer');
    res.setHeader('X-Content-Type-Options', 'nosniff');
    const fail = (status: number) => { res.writeHead(status); res.end('Invalid or expired authorization callback.'); };
    if (consumed || !expectedState || req.method !== 'GET' || req.headers.host !== `127.0.0.1:${port}` || !req.url?.startsWith('/auth/callback?') || req.url.length > 10 * 1024 || req.headers.origin) return fail(400);
    const raw = new URL(req.url, redirectURI);
    if (raw.pathname !== '/auth/callback' || raw.hash) return fail(400);
    const q = raw.searchParams;
    for (const key of ['state', 'code', 'client_id', 'iss', 'error']) if (q.getAll(key).length > 1) return fail(400);
    const state = q.get('state') ?? '', code = q.get('code') ?? '', error = q.get('error') ?? '';
    if (!/^[A-Za-z0-9_-]{43}$/.test(state) || !equal(state, expectedState) || (!code === !error) || code.length > 4096 || error.length > 128 || (q.get('client_id')?.length ?? 0) > 256 || (q.get('iss')?.length ?? 0) > 2048) return fail(400);
    // Unknown parameters, including tokens and provider error descriptions, are
    // deliberately not forwarded or rendered. Nothing from the URL is echoed.
    consumed = true;
    res.end('Authorization received. Return to Molr to check the result.');
    resolve({ State: state, Code: code, ClientID: q.get('client_id') ?? '', Issuer: q.get('iss') ?? '', Error: error });
    server.close();
  });
  server.maxConnections = 8;
  server.on('clientError', (_error, socket) => socket.destroy());
  const close = () => { server.close(); server.closeAllConnections(); reject(new Error('OAuth cancelled')); };
  signal.addEventListener('abort', close, { once: true });
  try {
    await new Promise<void>((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
  } catch { signal.removeEventListener('abort', close); close(); throw new Error('OAuth callback listener unavailable'); }
  const address = server.address();
  if (!address || typeof address === 'string' || signal.aborted) { close(); signal.removeEventListener('abort', close); throw new Error('OAuth cancelled'); }
  const port = address.port, redirectURI = `http://127.0.0.1:${port}/auth/callback`;
  return {
    redirectURI, response,
    arm(state: string) { if (!/^[A-Za-z0-9_-]{43}$/.test(state)) throw new Error('Invalid OAuth state'); expectedState = state; },
    close() { signal.removeEventListener('abort', close); close(); },
  };
}

function prepared(value: unknown, redirectURI: string): Started {
  const v = value as Started;
  if (!v || typeof v.ID !== 'string' || !identifier.test(v.ID) || typeof v.State !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(v.State) || typeof v.AuthorizationURL !== 'string' || v.AuthorizationURL.length > 8192) throw new Error('Invalid OAuth preparation');
  const url = new URL(v.AuthorizationURL), q = url.searchParams;
  if (url.protocol !== 'https:' || url.username || url.password || url.hash) throw new Error('Invalid OAuth provider URL');
  for (const [key, expected] of Object.entries({ redirect_uri: redirectURI, state: v.State, response_type: 'code', code_challenge_method: 'S256' })) if (q.getAll(key).length !== 1 || q.get(key) !== expected) throw new Error('Invalid OAuth parameters');
  if (q.getAll('code_challenge').length !== 1 || !/^[A-Za-z0-9_-]{43}$/.test(q.get('code_challenge') ?? '')) throw new Error('PKCE required');
  const expires = Date.parse(v.ExpiresAt);
  if (!Number.isFinite(expires) || expires <= Date.now() || expires > Date.now() + oauthLifetime + 30_000) throw new Error('Invalid OAuth expiry');
  return v;
}
function receipt(value: unknown, id: string): AppOAuthResult {
  const r = value as Receipt;
  if (!r || r.ID !== id) return { status: 'unknown', attemptId: id };
  if (r.Status === 'connected' && typeof r.ConnectionID === 'string' && identifier.test(r.ConnectionID)) return { status: 'connected', attemptId: id, connectionId: r.ConnectionID };
  if (r.Status === 'cancelled' || r.Status === 'failed') return { status: r.Status, attemptId: id };
  return { status: 'unknown', attemptId: id };
}

export class OAuthFlow {
  private active?: AbortController;
  cancel() { this.active?.abort(); }
  async authorize(provider: unknown, transport: OAuthTransport): Promise<AppOAuthResult> {
    if (typeof provider !== 'string' || !identifier.test(provider)) throw new Error('Invalid OAuth provider');
    if (this.active) throw new Error('OAuth authorization already active');
    const controller = new AbortController(); this.active = controller;
    const timer = setTimeout(() => controller.abort(), oauthLifetime);
    let listener: Awaited<ReturnType<typeof listenCallback>> | undefined, id: string | undefined, completing = false;
    try {
      listener = await listenCallback(controller.signal);
      const start = prepared(await transport.request('/api/oauth/start', { Provider: provider, RedirectURI: listener.redirectURI }, controller.signal), listener.redirectURI);
      id = start.ID;
      listener.arm(start.State);
      // Native confirmation identifies both the external authority and Runtime
      // destination, so a remote page cannot silently open an authorization flow.
      if (!await transport.confirm(new URL(start.AuthorizationURL).origin, controller.signal) || controller.signal.aborted) return { status: 'cancelled', attemptId: id };
      await transport.openExternal(start.AuthorizationURL);
      const callback = await listener.response;
      if (controller.signal.aborted) return { status: 'cancelled', attemptId: id };
      completing = true;
      try {
        return receipt(await transport.request('/api/oauth/complete', { ID: id, ...callback }, controller.signal), id);
      } catch {
        // Never repeat a token exchange after an ambiguous network response.
        try { return receipt(await transport.request(`/api/oauth/attempts/${id}`, undefined, controller.signal), id); }
        catch { return { status: 'unknown', attemptId: id }; }
      }
    } catch {
      return { status: controller.signal.aborted ? 'cancelled' : 'failed', ...(id ? { attemptId: id } : {}) };
    } finally {
      clearTimeout(timer); listener?.close();
      if (id && !completing) await transport.request('/api/oauth/cancel', { ID: id }, AbortSignal.timeout(3000)).catch(() => {});
      if (this.active === controller) this.active = undefined;
    }
  }
}
