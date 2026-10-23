import type { FileInfo, Page } from './types';

export class APIError extends Error {
  constructor(public code: string, public requestID = '', public status = 0, public retryAt = 0, public reason = '') { super(code); }
}
export function mutationKey() { return Array.from(crypto.getRandomValues(new Uint8Array(24)), byte => byte.toString(16).padStart(2, '0')).join(''); }

export function createClient() {
  const sessionEvents = new EventTarget();
  function onMutation(callback: (path: string) => void) { const changed = (event: Event) => callback((event as CustomEvent<string>).detail); sessionEvents.addEventListener('mutation', changed); return () => sessionEvents.removeEventListener('mutation', changed); }
  function onSessionExpired(callback: () => void) { sessionEvents.addEventListener('expired', callback); return () => sessionEvents.removeEventListener('expired', callback); }
  function onRequestSuccess(callback: () => void) { sessionEvents.addEventListener('success', callback); return () => sessionEvents.removeEventListener('success', callback); }
  let csrfCookieName = '';
  function csrfToken() {
    if (!csrfCookieName) return '';
    const value = document.cookie.split(';').map(cookie => cookie.trim()).find(cookie => cookie.startsWith(`${csrfCookieName}=`))?.slice(csrfCookieName.length + 1);
    return value ? decodeURIComponent(value) : '';
  }

  const attempts = new Map<string, string>();
  let cooldown: { identity: string; until: number; error: APIError } | undefined;
  let generation = 0;
  function clearClientState() { attempts.clear(); cooldown = undefined; generation++; }

  async function request<T>(path: string, init: RequestInit = {}, csrf?: string, binary = false, readOnly = false, expireOnUnauthorized = false): Promise<T> {
    if (!/^\/api\/[A-Za-z0-9/_-]+(?:\?[^#]*)?$/.test(path) || path.split('?')[0].includes('..') || path.includes('\\')) throw new APIError('invalid_argument');
    // Discover the active Runtime cookie before the first authenticated write.
    // Login supplies its separate pre-login proof explicitly.
    if (init.method && init.method !== 'GET' && csrf === undefined && !csrfCookieName) {
      await request('/api/auth/me', { signal: init.signal });
      if (!csrfCookieName) throw new APIError('invalid_response');
    }
    const headers = new Headers(init.headers);
    if (init.body && typeof init.body === 'string') headers.set('Content-Type', 'application/json');
    if (init.method && init.method !== 'GET') headers.set('X-CSRF-Token', csrf ?? csrfToken());
    const requestIdentity = csrfToken();
    const requestGeneration = generation;
    if ((readOnly || !init.method || init.method === 'GET') && !path.startsWith('/api/auth/') && cooldown?.identity === requestIdentity && cooldown.until > Date.now()) throw cooldown.error;
    let response: Response;
    try { response = await fetch(path, { ...init, headers, credentials: 'same-origin', cache: 'no-store' }); }
    catch (error) { if (error instanceof DOMException && error.name === 'AbortError') throw error; throw new APIError('unavailable'); }
    const cookieName = response.headers.get('X-Molr-CSRF-Cookie');
    if (cookieName && /^(?:__Host-molr_|molr_dev_|molr_lan_)csrf(?:_p[0-9]+)?$/.test(cookieName)) csrfCookieName = cookieName;
    let data: any;
    try { data = binary && response.ok ? await response.blob() : await response.json(); }
    catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') throw error;
      if (response.ok) throw new APIError('invalid_response', '', response.status);
    }
    if (!response.ok) {
      if (response.status === 401 && !init.signal?.aborted && requestIdentity === csrfToken() && (path !== '/api/auth/me' || expireOnUnauthorized) && !path.startsWith('/api/auth/login') && !path.startsWith('/api/auth/mfa/complete') && path !== '/api/auth/reauthenticate' && path !== '/api/auth/mfa/confirm') sessionEvents.dispatchEvent(new Event('expired'));
      const retryAfter = response.headers.get('Retry-After');
      const seconds = retryAfter && /^\d+$/.test(retryAfter) ? Number(retryAfter) : NaN;
      const retryAt = response.status === 429 ? Number.isFinite(seconds) ? Date.now() + seconds * 1000 : Math.max(0, Date.parse(retryAfter ?? '') || 0) : 0;
      const error = new APIError(data?.code ?? 'unavailable', data?.request_id ?? '', response.status, retryAt, typeof data?.reason === 'string' ? data.reason : '');
      if (response.status === 429 && !path.startsWith('/api/auth/')) cooldown = { identity: requestIdentity, until: retryAt || Date.now() + 60000, error };
      throw error;
    }
    // Only current, completed Runtime requests can wake a live subscription.
    // Authentication checks must not turn a failing SSE into a retry loop.
    if (!path.startsWith('/api/auth/') && !init.signal?.aborted && requestGeneration === generation && requestIdentity === csrfToken()) sessionEvents.dispatchEvent(new Event('success'));
    if (!readOnly && init.method && init.method !== 'GET') sessionEvents.dispatchEvent(new CustomEvent('mutation', { detail: path }));
    return data as T;
  }
  const workspacePath = (id: string, params: Record<string, string | number> = {}, content = false) => `/api/assistants/${encodeURIComponent(id)}/workspace${content ? '/content' : ''}?${new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)]))}`;
  const api = {
    workspace: {
      list: (id: string, path: string, offset = 0, version = '', signal?: AbortSignal) => request<import('./types').WorkspaceListing>(workspacePath(id, { path, offset, version }), { signal }),
      stat: (id: string, path: string, signal?: AbortSignal) => request<import('./types').WorkspaceEntry>(workspacePath(id, { action: 'stat', path }), { signal }),
      read: (id: string, path: string, signal?: AbortSignal) => request<import('./types').WorkspaceText>(workspacePath(id, { action: 'read', path }), { signal }),
      change: (id: string, operation: import('./types').WorkspaceOperation) => request<import('./types').WorkspaceEntry | null>(workspacePath(id), { method: 'POST', body: JSON.stringify(operation) }),
      upload: (id: string, path: string, file: Blob, version = '', signal?: AbortSignal) => request<import('./types').WorkspaceEntry>(workspacePath(id, { path, version }, true), { method: 'PUT', body: file, headers: { 'Content-Type': 'application/octet-stream' }, signal }),
      download: (id: string, path: string, version: string, signal?: AbortSignal) => request<Blob>(workspacePath(id, { path, version }, true), { signal }, undefined, true),
      downloadURL: (id: string, path: string, version: string) => workspacePath(id, { path, version }, true),
    },
    importPlugin: (file: File) => request<import('./plugins').PluginInstallation>('/api/plugins/install', { method: 'POST', body: file, headers: { 'Content-Type': 'application/zip' } }),
    download: (path: string, signal?: AbortSignal) => request<Blob>(path, { signal }, undefined, true),
    get: <T,>(path: string, signal?: AbortSignal) => request<T>(path, { signal }),
    put: <T,>(path: string, body: unknown) => request<T>(path, { method: 'PUT', body: JSON.stringify(body) }),
    post: <T,>(path: string, body: unknown = {}, csrf?: string) => request<T>(path, { method: 'POST', body: JSON.stringify(body) }, csrf),
    read: <T,>(path: string, body: unknown, signal?: AbortSignal) => request<T>(path, { method: 'POST', body: JSON.stringify(body), signal }, undefined, false, true),
    remove: (path: string) => request<null>(path, { method: 'DELETE' }),
    async mutate<T>(path: string, body: object, version: number | string = 0): Promise<T> {
      const sensitive = path.startsWith('/api/credentials') || 'verification' in body;
      const fingerprint = sensitive ? '' : JSON.stringify([path, body, version]);
      const key = attempts.get(fingerprint) ?? mutationKey();
      if (!sensitive) attempts.set(fingerprint, key);
      try {
        const value = await this.post<T>(path, { ...body, Mutation: { IdempotencyKey: key, ExpectedVersion: version || undefined } });
        attempts.delete(fingerprint); return value;
      } catch (error) {
        // Retry an uncertain operation with its original key, never as a duplicate.
        if (error instanceof APIError && error.status >= 400 && error.status < 500) attempts.delete(fingerprint);
        throw error;
      }
    },
    async all<T>(path: string, signal?: AbortSignal): Promise<T[]> {
      const result: T[] = []; let cursor = ''; const seen = new Set<string>();
      do {
        const params = new URLSearchParams(path.split('?')[1]); params.set('limit', '200'); if (cursor) params.set('cursor', cursor);
        const page = await this.get<Page<T>>(`${path.split('?')[0]}?${params}`, signal);
        result.push(...(page.Items ?? [])); cursor = page.NextCursor;
        if (cursor && seen.has(cursor)) throw new APIError('invalid_response');
        seen.add(cursor);
      } while (cursor);
      return result;
    },
    upload: (file: File, key: string) => request<FileInfo>('/api/files', { method: 'POST', body: file, headers: { 'Content-Type': file.type || 'application/octet-stream', 'X-File-Name': encodeURIComponent(file.name), 'X-File-Name-Encoding': 'percent', 'Idempotency-Key': key } }),
  };

  // Session restoration handles its own 401. A failed live connection instead
  // needs to notify subscribers, while retaining request identity and abort guards.
  const verifySession = (signal?: AbortSignal) => request<unknown>('/api/auth/me', { signal }, undefined, false, false, true);
  return { api, csrfToken, clearClientState, onMutation, onSessionExpired, onRequestSuccess, verifySession };
}
export const { api, csrfToken, clearClientState, onMutation, onSessionExpired, onRequestSuccess, verifySession } = createClient();
export type RuntimeClient = ReturnType<typeof createClient>['api'];
