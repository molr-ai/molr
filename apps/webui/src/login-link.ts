import { useSyncExternalStore } from 'react';
import { BrowserLogin } from './auth-device';

export type LoginLinkResult = { challenge?: string; target_path: string };
export type LoginLinkAttempt = { id: number; invalid: boolean; automatic: () => Promise<boolean>; preview: () => Promise<{ expires_at: string }>; redeem: () => Promise<{ result: LoginLinkResult }>; complete: (code: string) => Promise<void>; close: () => Promise<void> };
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
  let automatic: Promise<boolean> | undefined;
  void current?.close();
  const login = new BrowserLogin();
  current = { id: ++sequence, invalid,
    automatic() { return automatic ??= (window.molrApp?.consumeInitialLogin?.() ?? Promise.resolve(false)); },
    preview() { return preview ??= login.preview(token); },
    redeem() {
      request ??= (async () => {
        try { return { result: await login.start<LoginLinkResult>('/api/auth/login-links/redeem', { token }) }; }
        finally { token = ''; }
      })();
      return request;
    },
    complete: code => login.complete(code), close: () => login.close(),
  };
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
  void current?.close();
  current = null;
  listeners.forEach(listener => listener());
}
