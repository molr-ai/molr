import { createContext, useContext, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useModalScope, useToast } from '@molr/ui';
import { APIError } from './api/client';
import { useCopy, useData } from './state';

type Source = { toast: ReturnType<typeof useToast>['toast']; boundary: HTMLElement | null; title: string; retryLabel: string; retryAt: number; retry?: () => void };
type Group = { sources: Map<string, Source>; toastID?: number; boundary?: HTMLElement | null; dismissed: boolean; timer?: ReturnType<typeof setTimeout> };
const shared = (key: string) => key === 'unavailable' || key === 'rate_limited';

// Keep request lifecycle and deduplication here; the shared Toast owns all UI.
class ErrorToasts {
 private groups = new Map<string, Group>();
 constructor(private toasts: ReturnType<typeof useToast>) {}
 set(key: string, id: string, source: Source) {
  let group = this.groups.get(key);
  if (!group) { group = { sources: new Map(), dismissed: false }; this.groups.set(key, group); }
  group.sources.set(id, source);
  this.sync(key, group);
 }
 remove(key: string, id: string) {
  const group = this.groups.get(key);
  if (!group) return;
  group.sources.delete(id);
  this.sync(key, group);
  if (!group.sources.size && !shared(key)) this.groups.delete(key);
 }
 rearm = () => {
  for (const [key, group] of this.groups) if (shared(key) && group.dismissed) { group.dismissed = false; this.sync(key, group); }
 };
 private sync(key: string, group: Group) {
  clearTimeout(group.timer); group.timer = undefined;
  const source = [...group.sources.values()].at(-1);
  if (!source || group.dismissed) {
   if (group.toastID !== undefined) this.toasts.dismiss(group.toastID);
   group.toastID = undefined;
   return;
  }
  if (group.toastID !== undefined && group.boundary !== source.boundary) {
   this.toasts.dismiss(group.toastID); group.toastID = undefined;
  }
  group.boundary = source.boundary;
  const remaining = Math.max(0, Math.ceil((source.retryAt - Date.now()) / 1000));
  const options = {
   title: source.title, tone: key === 'rate_limited' ? 'warning' as const : 'danger' as const,
   // Retriable errors stay available; ordinary feedback uses Toast's timeout.
   duration: source.retry ? 0 : 5000,
   onDismiss: () => { group.dismissed = true; group.toastID = undefined; clearTimeout(group.timer); group.timer = undefined; },
   action: source.retry ? {
    label: source.retryLabel + (remaining ? ` (${remaining}s)` : ''), disabled: remaining > 0,
    onClick: () => { this.rearm(); source.retry?.(); },
   } : undefined,
  };
  if (group.toastID === undefined) group.toastID = source.toast(options);
  else this.toasts.update(group.toastID, options);
  if (source.retry && remaining) group.timer = setTimeout(() => this.sync(key, group), Math.min(1000, source.retryAt - Date.now()));
 }
 dispose = () => {
  for (const group of this.groups.values()) { clearTimeout(group.timer); if (group.toastID !== undefined) this.toasts.dismiss(group.toastID); }
  this.groups.clear();
 };
}
const Context = createContext<ErrorToasts | null>(null);
export function ErrorToastProvider({ children }: { children: ReactNode }) {
 const toast = useToast(), [errors] = useState(() => new ErrorToasts(toast));
 useEffect(() => errors.dispose, [errors]);
 return <Context.Provider value={errors}>{children}</Context.Provider>;
}
export function useErrorToasts() { return useContext(Context); }
export function useErrorToast(error: unknown, retry?: () => void) {
 const errors = useErrorToasts(), { t } = useCopy(), { hasConnectionNotice } = useData(), id = useId();
 const { toast } = useToast(), boundary = useModalScope()?.container ?? null;
 const retryRef = useRef(retry);
 useLayoutEffect(() => { retryRef.current = retry; });
 const code = error instanceof APIError ? error.code : 'unavailable';
 const reason = error instanceof APIError ? error.reason : '';
 const detail = Object.hasOwn(t.errorReasons, reason) ? t.errorReasons[reason as keyof typeof t.errorReasons] : undefined;
 const suppressed = !detail && hasConnectionNotice && code === 'unavailable';
 const key = error && !suppressed ? detail ? `${id}:${code}:${reason}` : shared(code) ? code : `${id}:${code}` : '';
 const title = detail ?? t.errors[code as keyof typeof t.errors] ?? t.serverUnavailable;
 const retryAt = error instanceof APIError ? error.retryAt : 0, hasRetry = !!retry;
 useEffect(() => () => { if (key) errors?.remove(key, id); }, [errors, key, id]);
 useEffect(() => {
  if (key) errors?.set(key, id, { toast, boundary, title, retryLabel: t.retry, retryAt, retry: hasRetry ? () => retryRef.current?.() : undefined });
 }, [errors, key, id, toast, boundary, title, t.retry, retryAt, hasRetry]);
}
