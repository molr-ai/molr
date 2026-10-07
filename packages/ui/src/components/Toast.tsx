import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { currentModalLayer, subscribeModalLayers } from '../overlay-layers';
import { getOverlayViewport, useModalScope } from '../modal-scope';
import type { ReactNode } from 'react';
import { useUILocale } from '../locale';
import type { Tone } from './feedback';
import { UIIcon } from './icons';
import { getTabStops } from '../focus';

export type ToastOptions = {
  title: ReactNode; description?: ReactNode; tone?: Tone; duration?: number;
  action?: { label: ReactNode; onClick: () => void };
};
type ToastEntry = ToastOptions & { id: number; origin: HTMLElement | null; boundary: HTMLElement | null };
type ToastContextValue = { toast: (options: ToastOptions, boundary?: HTMLElement | null) => number; dismiss: (id: number) => void; update: (id: number, options: Partial<ToastOptions>) => void };
const ToastContext = createContext<ToastContextValue | null>(null);

function ToastItem({ entry, dismiss, paused }: { entry: ToastEntry; dismiss: (id: number) => void; paused: boolean }) {
  const { messages } = useUILocale();
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [hidden, setHidden] = useState(() => typeof document === 'undefined' || document.hidden);
  const duration = entry.duration ?? (entry.action ? 0 : 5000);
  const remaining = useRef(duration);
  const root = useRef<HTMLLIElement>(null);
  useEffect(() => { remaining.current = duration; }, [duration]);
  useEffect(() => {
    const sync = () => setHidden(document.hidden);
    document.addEventListener('visibilitychange', sync);
    return () => document.removeEventListener('visibilitychange', sync);
  }, []);
  useEffect(() => {
    if (duration <= 0 || hovered || focused || hidden || paused) return;
    const started = Date.now();
    const timer = setTimeout(() => dismiss(entry.id), Math.max(0, remaining.current));
    return () => { clearTimeout(timer); remaining.current -= Date.now() - started; };
  }, [duration, hovered, focused, hidden, paused, entry.id, dismiss]);
  function close() {
    if (root.current?.contains(document.activeElement)) {
      const dialog = currentModalLayer(entry.boundary)?.dialog;
      const origin = entry.origin;
      const target = origin?.isConnected && origin.getClientRects().length && !origin.closest('[inert]') && (!dialog || dialog.contains(origin))
        ? origin : dialog?.querySelector<HTMLElement>('[tabindex="-1"], button');
      target?.focus({ preventScroll: true });
    }
    dismiss(entry.id);
  }
  const tone = entry.tone ?? 'neutral';
  return <li ref={root} className={`ui-toast ui-toast--${tone}`} onPointerEnter={() => setHovered(true)} onPointerLeave={() => setHovered(false)} onFocusCapture={() => setFocused(true)} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false); }}>
    <UIIcon name={tone === 'positive' ? 'check' : tone === 'danger' || tone === 'warning' ? 'warning' : 'info'} />
    <div className="ui-toast__body"><div role={tone === 'danger' ? 'alert' : 'status'} aria-atomic="true"><strong>{entry.title}</strong>{entry.description && <p>{entry.description}</p>}</div>
      {entry.action && <button className="ui-toast__action" type="button" onClick={() => { entry.action?.onClick(); close(); }}>{entry.action.label}</button>}
    </div>
    <button type="button" className="ui-icon-button" aria-label={messages.close} onClick={close}><UIIcon name="close" /></button>
  </li>;
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [entries, setEntries] = useState<ToastEntry[]>([]);
  const counter = useRef(0);
  const dismiss = useCallback((id: number) => setEntries(current => current.filter(entry => entry.id !== id)), []);
  const toast = useCallback((options: ToastOptions, boundary: HTMLElement | null = null) => {
    const id = ++counter.current;
    const origin = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setEntries(current => [...current, { ...options, id, origin, boundary }]);
    return id;
  }, []);
  const update = useCallback((id: number, options: Partial<ToastOptions>) => setEntries(current => current.map(entry => entry.id === id ? { ...entry, ...options } : entry)), []);
  const context = useMemo(() => ({ toast, dismiss, update }), [toast, dismiss, update]);
  const groups = new Map<HTMLElement | null, ToastEntry[]>();
  for (const entry of entries) groups.set(entry.boundary, [...groups.get(entry.boundary) ?? [], entry]);
  return <ToastContext.Provider value={context}>{children}{[...groups].map(([boundary, group]) =>
    <ToastViewport key={boundaryKey(boundary)} boundary={boundary} entries={group} dismiss={dismiss} />
  )}</ToastContext.Provider>;
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) throw new Error('useToast must be used inside ToastProvider.');
  const boundary = useModalScope()?.container ?? null;
  const toast = useCallback((options: ToastOptions) => context.toast(options, boundary), [context, boundary]);
  return useMemo(() => ({ toast, dismiss: context.dismiss, update: context.update }), [context, toast]);
}

const boundaryKeys = new WeakMap<HTMLElement, number>();
let nextBoundary = 0;
function boundaryKey(boundary: HTMLElement | null) {
  if (!boundary) return 'document';
  if (!boundaryKeys.has(boundary)) boundaryKeys.set(boundary, ++nextBoundary);
  return boundaryKeys.get(boundary)!;
}

function ToastViewport({ entries, boundary, dismiss }: { entries: ToastEntry[]; boundary: HTMLElement | null; dismiss: (id: number) => void }) {
  const { messages } = useUILocale();
  const [host, setHost] = useState<HTMLOListElement | null>(null);
  useLayoutEffect(() => { setHost(document.createElement('ol')); }, []);
  const [paused, setPaused] = useState(false);
  const sync = useRef<() => void>(() => {});
  useLayoutEffect(() => {
    if (!host) return;
    const element = host;
    element.className = 'ui-toasts';
    element.setAttribute('popover', 'manual');
    let disposed = false;
    function position() {
      const viewport = getOverlayViewport(boundary);
      const available = (!boundary || boundary.isConnected) && viewport.width > 0 && viewport.height > 0;
      setPaused(!available);
      element.style.right = `${Math.max(0, window.innerWidth - viewport.left - viewport.width)}px`;
      element.style.bottom = `${Math.max(0, window.innerHeight - viewport.top - viewport.height)}px`;
      element.style.setProperty('--toast-width', `${viewport.width}px`);
      element.style.setProperty('--toast-height', `${viewport.height}px`);
      if (!available && element.matches(':popover-open')) element.hidePopover();
      return available;
    }
    function synchronize() {
      if (disposed) return;
      const parent = currentModalLayer(boundary)?.dialog ?? (boundary?.isConnected ? boundary : document.body);
      const focused = element.contains(document.activeElement) ? document.activeElement as HTMLElement : null;
      // Reparent the same element: React portal children, timer budgets and focus survive.
      if (element.parentElement !== parent) {
        if (element.matches(':popover-open')) element.hidePopover();
        parent.append(element);
      }
      if (position()) {
        // showPopover on an already open element does not change top-layer order.
        if (element.matches(':popover-open')) element.hidePopover();
        element.showPopover();
        focused?.focus({ preventScroll: true });
      }
    }
    sync.current = synchronize;
    const unsubscribe = subscribeModalLayers(synchronize);
    const layerToggle = (event: Event) => {
      if (!(event.target instanceof HTMLElement) || event.target.classList.contains('ui-toasts')) return;
      if (event.target.matches('[popover], dialog')) synchronize();
    };
    const reposition = () => { if (position() && !element.matches(':popover-open')) synchronize(); };
    const observer = new ResizeObserver(reposition);
    if (boundary) observer.observe(boundary);
    document.addEventListener('toggle', layerToggle, true);
    window.addEventListener('resize', reposition);
    window.addEventListener('scroll', reposition, true);
    window.visualViewport?.addEventListener('resize', reposition);
    window.visualViewport?.addEventListener('scroll', reposition);
    // Portal React events follow the provider tree, not the dialog's DOM ancestry.
    const keydown = (event: KeyboardEvent) => {
      const dialog = currentModalLayer(boundary)?.dialog;
      if (!dialog) return;
      if (event.key === 'Escape' && !event.defaultPrevented) {
        event.preventDefault(); event.stopPropagation();
        dialog.dispatchEvent(new Event('cancel', { cancelable: true }));
      }
      if (event.key !== 'Tab' || event.defaultPrevented) return;
      const stops = getTabStops(dialog);
      const target = event.shiftKey && document.activeElement === stops[0] ? stops.at(-1) : !event.shiftKey && document.activeElement === stops.at(-1) ? stops[0] : null;
      if (target) { event.preventDefault(); target.focus(); }
    };
    element.addEventListener('keydown', keydown);
    synchronize();
    return () => {
      disposed = true; sync.current = () => {}; unsubscribe(); observer.disconnect();
      document.removeEventListener('toggle', layerToggle, true);
      window.removeEventListener('resize', reposition); window.removeEventListener('scroll', reposition, true);
      window.visualViewport?.removeEventListener('resize', reposition); window.visualViewport?.removeEventListener('scroll', reposition);
      element.removeEventListener('keydown', keydown);
      if (element.matches(':popover-open')) element.hidePopover();
      element.remove();
    };
  }, [host, boundary]);
  useLayoutEffect(() => { if (host) { host.setAttribute('aria-label', messages.notifications); sync.current(); } }, [host, entries, messages.notifications]);
  return host ? createPortal(entries.map(entry => <ToastItem key={entry.id} entry={entry} dismiss={dismiss} paused={paused} />), host) : null;
}
