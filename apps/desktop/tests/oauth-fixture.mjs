// Artificial Runtime/provider for native transport tests, never shipped in the app.
import { createServer } from 'node:http';
import { randomBytes, createHash } from 'node:crypto';
const random = () => randomBytes(32).toString('base64url');
export async function oauthFixture({ port = 0, host = '127.0.0.1' } = {}) {
  const attempts = new Map(), codes = new Map();
  let sessionID = 'fixture-session', exchanges = 0, stored = null;
  let control;
  const server = createServer(async (req, res) => {
    const url = new URL(req.url, `http://${req.headers.host}`);
    res.setHeader('Cache-Control', 'no-store');
    res.setHeader('Content-Type', 'application/json');
    const send = (status, value) => { res.writeHead(status); res.end(JSON.stringify(value)); };
    try {
      if (url.pathname.startsWith('/control')) return await control?.(req, res, url);
      if (url.pathname === '/') {
        res.setHeader('Content-Type', 'text/html');
        res.setHeader('Set-Cookie', ['fixture_session=synthetic; HttpOnly; SameSite=Strict; Path=/', 'molr_dev_csrf=fixture-proof; SameSite=Strict; Path=/']);
        return res.end('<!doctype html><meta charset="utf-8"><title>OAuth 模拟 Runtime</title><h1>OAuth 模拟 Runtime</h1><p>仅使用人工凭据，未连接真实服务。</p><button id="authorize">授权模拟服务</button> <button id="cancel">取消授权</button><pre id="result">ready</pre><script>window.molrApp.getInfo();document.querySelector("#authorize").onclick=async()=>{document.querySelector("#result").textContent="pending";window.result=await window.molrApp.oauth.authorize("fixture");document.querySelector("#result").textContent=JSON.stringify(window.result)};document.querySelector("#cancel").onclick=()=>window.molrApp.oauth.cancel()</script>');
      }
      if (url.pathname === '/provider/token') {
        let raw = ''; for await (const chunk of req) raw += chunk;
        const q = new URLSearchParams(raw), record = codes.get(q.get('code'));
        codes.delete(q.get('code'));
        if (!record || createHash('sha256').update(q.get('code_verifier') ?? '').digest('base64url') !== record.challenge || q.get('redirect_uri') !== record.redirect) return send(400, { error: 'invalid_grant' });
        exchanges++; return send(200, { access_token: 'synthetic-access-token', refresh_token: 'synthetic-refresh-token', scope: 'fixture.read' });
      }
      if (!req.headers.cookie?.includes('fixture_session=synthetic')) return send(401, {});
      res.setHeader('X-Molr-CSRF-Cookie', 'molr_dev_csrf');
      if (url.pathname === '/api/auth/me') return send(200, { OwnerID: 'fixture-owner', SubjectID: 'fixture-owner', Kind: 'owner', SessionID: sessionID, TokenID: '' });
      if (req.method === 'POST' && (req.headers['x-csrf-token'] !== 'fixture-proof' || req.headers.origin !== url.origin)) return send(403, {});
      let raw = ''; for await (const chunk of req) { raw += chunk; if (raw.length > 16384) return send(413, {}); }
      const input = raw ? JSON.parse(raw) : {};
      if (url.pathname === '/api/oauth/start') {
        if (input.Provider !== 'fixture') return send(400, {});
        const ID = random(), State = random(), verifier = random();
        const challenge = createHash('sha256').update(verifier).digest('base64url');
        const AuthorizationURL = 'https://oauth.fixture.invalid/authorize?' + new URLSearchParams({ redirect_uri: input.RedirectURI, state: State, response_type: 'code', code_challenge_method: 'S256', code_challenge: challenge, nonce: random() });
        const value = { ID, State, AuthorizationURL, ExpiresAt: new Date(Date.now() + 240_000).toISOString() };
        attempts.set(ID, { ...value, verifier, sessionID, redirect: input.RedirectURI, result: { ID, Status: 'pending' } });
        return send(200, value);
      }
      const a = attempts.get(input.ID ?? url.pathname.split('/').at(-1));
      if (!a || a.sessionID !== sessionID) return send(404, {});
      if (url.pathname === '/api/oauth/cancel') { if (a.result.Status === 'pending') a.result.Status = 'cancelled'; return send(200, a.result); }
      if (url.pathname.startsWith('/api/oauth/attempts/')) return send(200, a.result);
      if (url.pathname === '/api/oauth/complete') {
        if (input.State !== a.State) return send(403, {});
        if (a.result.Status !== 'pending') return send(200, a.result);
        if (input.Error) { a.result.Status = 'cancelled'; return send(200, a.result); }
        a.result.Status = 'completing';
        const token = await fetch(`http://127.0.0.1:${server.address().port}/provider/token`, { method: 'POST', body: new URLSearchParams({ code: input.Code, code_verifier: a.verifier, redirect_uri: a.redirect }) });
        if (!token.ok) { a.result.Status = 'failed'; return send(200, a.result); }
        stored = await token.json(); // Fake Runtime memory only; not exposed to pages.
        a.verifier = ''; a.result = { ID: a.ID, Status: 'connected', ConnectionID: 'fixture-connection' };
        return send(200, a.result);
      }
      send(404, {});
    } catch { if (!res.headersSent) send(500, { error: 'fixture_failed' }); else res.end(); }
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(port, host, resolve); });
  return {
    server, port: server.address().port,
    setControl(handler) { control = handler; },
    rotateSession() { sessionID = random(); },
    get exchanges() { return exchanges; },
    get hasCredentials() { return stored?.refresh_token === 'synthetic-refresh-token'; },
    async approve(raw, denied = false) {
      const url = new URL(raw); if (url.origin !== 'https://oauth.fixture.invalid') throw Error('Unexpected provider');
      const q = url.searchParams, code = random();
      codes.set(code, { challenge: q.get('code_challenge'), redirect: q.get('redirect_uri') });
      const target = q.get('redirect_uri') + '?' + new URLSearchParams({ state: q.get('state'), ...(denied ? { error: 'access_denied' } : { code }) });
      const response = await fetch(target); await response.text(); return response.status;
    },
    async close() { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); },
  };
}
