import { useSyncExternalStore } from 'react';
import { api } from './api/client';

export type LoginLinkResult = { challenge?: string; target_path: string };
export type LoginLinkAttempt = { id: number; invalid: boolean; preview: () => Promise<{ expires_at: string }>; redeem: () => Promise<{ result: LoginLinkResult; csrf: string }> };
let sequence = 0;
let current: LoginLinkAttempt | null = null;
const listeners = new Set<() => void>();

function capture() {
  if (!/^#\/?login(?:\?|$)/.test(location.hash)) return;
  let token = new URLSearchParams(location.hash.split('?')[1] ?? '').get('token') ?? '';
  // Scrub the address before authentication or rendering. Never persist the raw credential.
  history.replaceState(history.state, '', `${location.pathname}${location.search}#/login`);
  const invalid = !/^[A-Za-z0-9_-]{32,128}$/.test(token);
  let request: ReturnType<LoginLinkAttempt['redeem']> | undefined;
  let preview: ReturnType<LoginLinkAttempt['preview']> | undefined;
  current = { id: ++sequence, invalid, preview() {
    preview ??= (async () => {
      const proof = await api.get<{ csrf_token: string }>('/api/auth/csrf');
      return api.post<{ expires_at: string }>('/api/auth/login-links/preview', { token }, proof.csrf_token);
    })();
    return preview;
  }, redeem() {
    // One request per navigation, including React StrictMode's repeated effects.
    request ??= (async () => {
      const proof = await api.get<{ csrf_token: string }>('/api/auth/csrf');
      try {
        const result = await api.post<LoginLinkResult>('/api/auth/login-links/redeem', { token, device_name: 'Molr Web · Login link' }, proof.csrf_token);
        return { result, csrf: proof.csrf_token };
      } finally { token = ''; }
    })();
    return request;
  } };
  listeners.forEach(listener => listener());
}
capture();
window.addEventListener('hashchange', capture);
export function useLoginLink() {
  return useSyncExternalStore(listener => { listeners.add(listener); return () => listeners.delete(listener); }, () => current, () => null);
}
export function finishLoginLink(target = '/assistants') {
  // The canonical target comes from Runtime, never from the link's query string.
  if (['/settings/theme', '/settings/memory', '/settings/security'].includes(target)) target = target.replace('/settings/', '/account/');
  const allowed = ['/account/usage', '/settings/usage', '/account', '/account/theme', '/account/memory', '/account/security', '/assistants', '/plans', '/resources', '/settings', '/settings/theme', '/extensions', '/settings/memory', '/settings/security', '/workbench'];
  history.replaceState(history.state, '', `${location.pathname}${location.search}#${allowed.includes(target) ? target : '/assistants'}`);
  current = null;
  listeners.forEach(listener => listener());
}
