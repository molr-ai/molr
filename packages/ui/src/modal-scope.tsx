import { createContext, useCallback, useContext, useMemo, useRef } from 'react';
import type { ReactNode } from 'react';

export type ModalScopeValue = {
  container: HTMLElement | null;
  presentation: 'sheet' | 'dialog';
  register: (layer: HTMLElement, origin: HTMLElement | null, parentDialog?: HTMLElement | null) => () => void;
  resolveFocus: (target: HTMLElement | null) => HTMLElement | null;
};
const ModalScopeContext = createContext<ModalScopeValue | null>(null);

/** Keep modal sheets and their backdrops inside an embedded application. */
export function ModalScope({ container, enabled = true, presentation = 'sheet', children }: { container: HTMLElement | null; enabled?: boolean; presentation?: 'sheet' | 'dialog'; children: ReactNode }) {
  const content = useRef<HTMLDivElement>(null);
  const layers = useRef<HTMLElement[]>([]);
  const parents = useRef(new WeakMap<HTMLElement, HTMLElement | null>());
  const originalFocus = useRef<HTMLElement | null>(null);
  const attachContent = useCallback((element: HTMLDivElement | null) => {
    content.current = element;
    // Activity detaches refs before layer cleanup. Reconcile the preserved DOM
    // when it is revealed, even if no dialog needs to register again.
    if (element) element.inert = layers.current.length > 0;
  }, []);
  const register = useCallback((layer: HTMLElement, origin: HTMLElement | null, parentDialog: HTMLElement | null = null) => {
    function synchronize() {
      if (content.current) content.current.inert = layers.current.length > 0;
      layers.current.forEach((element, index) => {
        element.style.zIndex = String(100 + index);
        element.inert = index !== layers.current.length - 1;
      });
    }
    if (!layers.current.length) originalFocus.current = origin;
    parents.current.set(layer, parentDialog);
    const dialog = layer.querySelector('dialog');
    const childIndex = layers.current.findIndex(existing => parents.current.get(existing) === dialog);
    if (childIndex < 0) layers.current.push(layer);
    else layers.current.splice(childIndex, 0, layer);
    synchronize();
    return () => {
      layers.current = layers.current.filter(element => element !== layer);
      parents.current.delete(layer);
      layer.inert = false;
      synchronize();
    };
  }, []);
  const resolveFocus = useCallback((target: HTMLElement | null) => {
    const available = (element: HTMLElement | null) => element?.isConnected && element.getClientRects().length && !element.closest('[inert]') && !element.matches(':disabled');
    if (available(target)) return target;
    // A nested action can remove its own trigger. Keep focus in the remaining
    // sheet before considering the original entry outside all modal layers.
    const remaining = layers.current.at(-1)?.querySelector<HTMLElement>('dialog[open] [tabindex="-1"]') ?? null;
    return available(remaining) ? remaining : available(originalFocus.current) ? originalFocus.current : null;
  }, []);
  const scope = useMemo(() => ({ container, presentation, register, resolveFocus }), [container, presentation, register, resolveFocus]);
  return <ModalScopeContext.Provider value={enabled ? scope : null}>
    <div ref={attachContent} className="ui-modal-scope-content">{children}</div>
  </ModalScopeContext.Provider>;
}

export function useModalScope() { return useContext(ModalScopeContext); }

export function getOverlayViewport(container?: HTMLElement | null) {
  if (typeof window === 'undefined') return { left: 0, top: 0, width: 0, height: 0 };
  const viewport = window.visualViewport;
  let left = viewport?.offsetLeft ?? 0, top = viewport?.offsetTop ?? 0;
  let right = left + (viewport?.width ?? document.documentElement.clientWidth);
  let bottom = top + (viewport?.height ?? window.innerHeight);
  if (container) {
    const rect = container.getBoundingClientRect();
    left = Math.max(left, rect.left + container.clientLeft);
    top = Math.max(top, rect.top + container.clientTop);
    right = Math.min(right, rect.left + container.clientLeft + container.clientWidth);
    bottom = Math.min(bottom, rect.top + container.clientTop + container.clientHeight);
  }
  return { left, top, width: Math.max(0, right - left), height: Math.max(0, bottom - top) };
}
