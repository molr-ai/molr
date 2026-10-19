import assert from 'node:assert/strict';
import { test } from 'node:test';
import { randomBytes } from 'node:crypto';
import { request as httpRequest } from 'node:http';
import { OAuthFlow, listenCallback, oauthLifetime } from '../dist/main/main/oauth.js';
import { runtimeOAuthTransport } from '../dist/main/main/oauth-transport.js';

const state = () => randomBytes(32).toString('base64url');
function prepare(body) {
  const State = state();
  return { ID: state(), State, ExpiresAt: new Date(Date.now() + 240_000).toISOString(), AuthorizationURL: 'https://provider.example/authorize?' + new URLSearchParams({ redirect_uri: body.RedirectURI, state: State, response_type: 'code', code_challenge_method: 'S256', code_challenge: state() }) };
}
test('loopback listener validates Host, method, state, duplicates and closes after use', async () => {
  const controller = new AbortController(), listener = await listenCallback(controller.signal), expected = state();
  listener.arm(expected);
  const callback = `${listener.redirectURI}?state=${expected}&code=fixture-code`;
  try {
    assert.equal((await fetch(callback.replace(expected, state()))).status, 400);
    assert.equal((await fetch(callback + '&code=second')).status, 400);
    assert.equal((await fetch(callback, { method: 'POST' })).status, 400);
    assert.equal((await fetch(callback, { headers: { Origin: 'https://attacker.example' } })).status, 400);
    const wrongHost = await new Promise(resolve => { const req = httpRequest(callback, { headers: { Host: 'attacker.example' } }, res => { res.resume(); resolve(res.statusCode); }); req.end(); });
    assert.equal(wrongHost, 400);
    const response = await fetch(callback + '&access_token=never-forward');
    assert.equal(response.status, 200); assert.equal(response.headers.get('cache-control'), 'no-store');
    assert(!((await response.text()).includes('fixture-code')));
    assert.deepEqual(await listener.response, { State: expected, Code: 'fixture-code', ClientID: '', Issuer: '', Error: '' });
  } finally { listener.close(); }
  await assert.rejects(fetch(callback));
});
test('cancel releases listener without receiving a callback', async () => {
  const controller = new AbortController(), listener = await listenCallback(controller.signal);
  controller.abort(); await assert.rejects(listener.response);
  await assert.rejects(fetch(listener.redirectURI)); listener.close();
});
test('timeout cancels a pending attempt, releases the port and rejects concurrent flows', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const flow = new OAuthFlow(); let ready, redirect, cancelled = false;
  const opened = new Promise(resolve => { ready = resolve; });
  const transport = {
    async request(path, body) { if (path.endsWith('/start')) { redirect = body.RedirectURI; return prepare(body); } cancelled = true; return {}; },
    async confirm() { return true; }, async openExternal() { ready(); },
  };
  const result = flow.authorize('fixture', transport);
  await opened;
  await assert.rejects(flow.authorize('fixture', transport), /already active/);
  t.mock.timers.tick(oauthLifetime);
  assert.equal((await result).status, 'cancelled'); assert(cancelled);
  t.mock.timers.reset();
  await assert.rejects(fetch(redirect));
});
test('flow forwards code directly, returns a sanitized receipt and handles lost completion responses', async () => {
  for (const lostResponse of [false, true]) {
    const flow = new OAuthFlow(); let start, complete = 0;
    const result = await flow.authorize('fixture', {
      async request(path, body) {
        if (path.endsWith('/start')) return start = prepare(body);
        if (path.endsWith('/complete')) { complete++; assert.equal(body.Code, 'fake-code'); assert.equal(body.State, start.State); if (lostResponse) throw Error('token=private'); }
        return { ID: start.ID, Status: 'connected', ConnectionID: 'saved-connection', AccessToken: 'must-not-leak' };
      },
      async confirm(origin) { assert.equal(origin, 'https://provider.example'); return true; },
      async openExternal(raw) { const q = new URL(raw).searchParams; await fetch(q.get('redirect_uri') + '?' + new URLSearchParams({ state: q.get('state'), code: 'fake-code' })); },
    });
    assert.deepEqual(result, { status: 'connected', attemptId: start.ID, connectionId: 'saved-connection' }); assert.equal(complete, 1);
  }
});
test('declining, cancelling and malformed provider replies never exchange a code', async () => {
  for (const mode of ['decline', 'cancel', 'invalid']) {
    const flow = new OAuthFlow(); let start, cancelled = 0, opened = false;
    const result = await flow.authorize('fixture', {
      async request(path, body) {
        if (path.endsWith('/start')) { start = prepare(body); if (mode === 'invalid') start.AuthorizationURL = 'file:///etc/passwd'; return start; }
        if (path.endsWith('/cancel')) { cancelled++; return {}; }
        assert.fail('unexpected exchange');
      },
      async confirm() { return mode !== 'decline'; },
      async openExternal() { opened = true; flow.cancel(); },
    });
    assert.equal(result.status, mode === 'invalid' ? 'failed' : 'cancelled');
    assert.equal(opened, mode === 'cancel');
    assert.equal(cancelled, mode === 'invalid' ? 0 : 1);
  }
});
test('transport allows opted-in LAN HTTP, preserves CSRF and rejects session switching', async () => {
  let sessionID = 'session-one', posts = 0;
  const transport = runtimeOAuthTransport({ url: 'http://192.168.1.2:8787', allowHTTP: true, current: () => true,
    session: {
      cookies: { get: async () => [{ value: 'proof' }] },
      async fetch(url, options) {
        assert.equal(options.redirect, 'error'); assert.equal(options.headers.Origin, 'http://192.168.1.2:8787');
        const headers = { 'Content-Type': 'application/json', 'X-Molr-CSRF-Cookie': 'molr_lan_csrf_p8787' };
        if (url.endsWith('/api/auth/me')) return new Response(JSON.stringify({ OwnerID: 'owner', SessionID: sessionID, Kind: 'owner' }), { headers });
        posts++; assert.equal(options.headers['X-CSRF-Token'], 'proof'); return new Response('{}', { headers });
      },
    }, confirm: async () => true, openExternal: async () => {},
  });
  await transport.request('/api/oauth/start', {}); assert.equal(posts, 1);
  sessionID = 'session-two'; await assert.rejects(transport.request('/api/oauth/complete', {}), /login changed/); assert.equal(posts, 1);
  assert.throws(() => runtimeOAuthTransport({ url: 'http://public.example', allowHTTP: true }), /HTTPS/);
  assert.throws(() => runtimeOAuthTransport({ url: 'http://192.168.1.2:8787', allowHTTP: false }), /HTTPS/);
});
