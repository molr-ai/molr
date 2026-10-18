import { Children, createContext, useContext, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { ReactNode, RefObject } from 'react';
import { useUILocale } from '../locale';
import { UIIcon } from './icons';
import { useModalScope } from '../modal-scope';
import { registerModalLayer } from '../overlay-layers';
import { getTabStops } from '../focus';
import { ModalPresenceContext } from '../modal-presence';
import { suspendTooltips } from './tooltip-activity';

function closeDialog(dialog: HTMLDialogElement) {
  // Native close() can restore trigger focus before our explicit restoration.
  suspendTooltips();
  dialog.close();
}

export type DialogProps = {
  open: boolean; onOpenChange: (open: boolean) => void; onAfterClose?: () => void; title: string; description?: string;
  children?: ReactNode; footer?: ReactNode; closeLabel?: string; size?: 'sm' | 'md' | 'lg';
  headerContent?: ReactNode;
  initialFocusRef?: RefObject<HTMLElement | null>; dismissible?: boolean; showCloseButton?: boolean; className?: string;
};

const ModalParentContext = createContext<RefObject<HTMLDialogElement | null> | null>(null);

type ModalProps = DialogProps & { drawer?: boolean; drawerSide?: 'right' | 'bottom'; docked?: boolean };

function Modal(props: ModalProps) {
  const presence = useContext(ModalPresenceContext);
  const open = props.open && presence?.present !== false;
  const retained = useRef(props);
  const [exited, setExited] = useState(!open);
  if (open && exited) setExited(false);
  useLayoutEffect(() => { if (open || exited) retained.current = props; });
  // Closing must preserve the previous title, body and footer, even if the caller
  // clears its selection in the same render. Layout mode stays responsive.
  const { title, description, children, footer, headerContent } = open || exited ? props : retained.current;
  return <ModalView {...props} {...{ open, title, description, children, footer, headerContent }} onAfterClose={() => { setExited(true); props.onAfterClose?.(); }} />;
}

function ModalView({ open, onOpenChange, onAfterClose, title, description, children, footer, headerContent, closeLabel, size = 'md', initialFocusRef, dismissible = true, showCloseButton = true, className = '', drawer: requestedDrawer = false, drawerSide: requestedSide = 'right', docked }: ModalProps) {
  const presence = useContext(ModalPresenceContext);
  const releasePresence = useRef<(() => void) | undefined>(undefined);
  function release() { releasePresence.current?.(); releasePresence.current = undefined; }
  const afterClose = useRef(onAfterClose), closePending = useRef(false);
  useLayoutEffect(() => { afterClose.current = onAfterClose; });
  function reportClosed() { if (closePending.current) { closePending.current = false; afterClose.current?.(); } }
  const scope = useModalScope();
  const parentDialog = useContext(ModalParentContext);
  const contained = !!scope?.container && !docked;
  const drawer = contained && scope?.presentation !== 'dialog' || requestedDrawer;
  const drawerSide = contained && scope?.presentation !== 'dialog' ? 'bottom' : requestedSide;
  const id = useId();
  const ref = useRef<HTMLDialogElement>(null);
  // Opt-in drawers keep one portal target across docked/native/contained modes.
  // Moving its host preserves the mounted forms, tab state and DOM selections.
  const [host] = useState(() => docked !== undefined && typeof document !== 'undefined' ? document.createElement('div') : null);
  const anchorRef = useRef<HTMLSpanElement>(null);
  const modeFocus = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => {
    if (!host) return;
    host.style.display = 'contents';
    const target = contained ? scope?.container : anchorRef.current?.parentElement;
    if (target && host.parentElement !== target) target.appendChild(host);
  }, [host, contained, scope?.container]);
  useLayoutEffect(() => () => { host?.remove(); }, [host]);
  const layerRef = useRef<HTMLDivElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);
  const unregisterToast = useRef<(() => void) | null>(null);
  const unregister = useRef<(() => void) | null>(null);
  const [present, setPresent] = useState(false);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const startedOutside = useRef(false);
  const interruptedMotion = useRef<{ transform: string; opacity: string } | null>(null);
  const { messages, reducedMotion: preferReducedMotion } = useUILocale();
  function restoreFocus() {
    // A docked panel does not own focus outside itself while it collapses.
    if (docked && document.activeElement !== document.body && !ref.current?.contains(document.activeElement)) { returnFocus.current = null; return; }
    const target = scope ? scope.resolveFocus(returnFocus.current) : returnFocus.current;
    returnFocus.current = null;
    if (target?.isConnected) { suspendTooltips(); target.focus({ preventScroll: true }); }
  }
  useLayoutEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    const entering = open && !dialog.open;
    if (entering) {
      closePending.current = true;
      releasePresence.current = presence?.register();
      interruptedMotion.current = null;
      returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      if (contained && layerRef.current) {
        setPresent(true);
        unregister.current = scope!.register(layerRef.current, returnFocus.current, parentDialog?.current ?? null);
        dialog.show();
      } else if (docked) dialog.show();
      else dialog.showModal();
      if (!docked) unregisterToast.current = registerModalLayer(dialog, contained ? scope!.container : null, parentDialog?.current ?? null);
      (modeFocus.current?.isConnected ? modeFocus.current : initialFocusRef?.current ?? titleRef.current)?.focus({ preventScroll: true });
      modeFocus.current = null;
    }
    if (docked || !drawer || !dialog.open) {
      if (!open) {
        if (dialog.open) closeDialog(dialog);
        unregisterToast.current?.(); unregisterToast.current = null;
        unregister.current?.(); unregister.current = null;
        setPresent(false);
        restoreFocus();
        reportClosed();
        release();
      }
      return;
    }

    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
    const animations: Animation[] = [];
    let active = true;
    let settled = false;
    function finish() {
      if (!active || settled || !dialog) return;
      settled = true;
      interruptedMotion.current = null;
      // Keep the contained backdrop at its final opacity after animation cleanup,
      // including the frame before React hides the layer at the end of an exit.
      if (contained && backdropRef.current) backdropRef.current.style.opacity = open ? '1' : '0';
      // Keep the native modal and its focus containment until the exit ends.
      if (!open) {
        closeDialog(dialog);
        unregisterToast.current?.(); unregisterToast.current = null;
        unregister.current?.(); unregister.current = null;
        setPresent(false);
        restoreFocus();
        reportClosed();
        release();
      }
      for (const animation of animations) animation.cancel();
    }
    if (reducedMotion.matches || preferReducedMotion) { finish(); return; }

    const offscreen = drawerSide === 'bottom' ? 'translateY(100%)' : 'translateX(100%)';
    const from = interruptedMotion.current ?? {
      transform: entering ? offscreen : getComputedStyle(dialog).transform,
      opacity: entering ? '0' : contained && backdropRef.current ? getComputedStyle(backdropRef.current).opacity : getComputedStyle(dialog, '::backdrop').opacity,
    };
    interruptedMotion.current = null;
    const timing: KeyframeAnimationOptions = {
      duration: open ? 280 : 220,
      easing: open ? 'cubic-bezier(0.22, 1, 0.36, 1)' : 'cubic-bezier(0.4, 0, 1, 1)',
      fill: 'forwards',
    };
    animations.push(dialog.animate([{ transform: from.transform }, { transform: open ? 'translate(0, 0)' : offscreen }], { ...timing, id: 'molr-drawer-slide' }));
    try {
      const keyframes = [{ opacity: from.opacity }, { opacity: open ? 1 : 0 }];
      animations.push(contained && backdropRef.current
        ? backdropRef.current.animate(keyframes, { ...timing, id: 'molr-drawer-backdrop' })
        : dialog.animate(keyframes, { ...timing, id: 'molr-drawer-backdrop', pseudoElement: '::backdrop' }));
    } catch {
      // Sliding still works in browsers without pseudo-element animation support.
    }
    void Promise.all(animations.map(animation => animation.finished)).then(finish, finish);
    const synchronizeMotion = () => { if (reducedMotion.matches) finish(); };
    reducedMotion.addEventListener('change', synchronizeMotion);
    return () => {
      active = false;
      if (!settled && dialog.open) {
        // A rapid reversal continues from the actual on-screen position.
        interruptedMotion.current = { transform: getComputedStyle(dialog).transform, opacity: contained && backdropRef.current ? getComputedStyle(backdropRef.current).opacity : getComputedStyle(dialog, '::backdrop').opacity };
      }
      for (const animation of animations) animation.cancel();
      reducedMotion.removeEventListener('change', synchronizeMotion);
    };
  }, [open, drawer, drawerSide, docked, initialFocusRef, contained, scope, preferReducedMotion, presence?.register]);
  useLayoutEffect(() => {
    const dialog = ref.current;
    return () => {
      const wasOpen = dialog?.open;
      const shouldRestoreFocus = !docked || document.activeElement === document.body || !!dialog?.contains(document.activeElement);
      if (host && dialog?.contains(document.activeElement)) modeFocus.current = document.activeElement as HTMLElement;
      if (wasOpen) closeDialog(dialog);
      release();
      unregisterToast.current?.(); unregisterToast.current = null;
      unregister.current?.(); unregister.current = null;
      if (wasOpen && shouldRestoreFocus) restoreFocus();
      else returnFocus.current = null;
    };
  }, [docked, contained, scope?.container]);
  useLayoutEffect(() => {
    const dialog = ref.current;
    if (!dialog || !open) return;
    let focused: HTMLElement | null = dialog.contains(document.activeElement) ? document.activeElement as HTMLElement : null;
    const focus = (event: FocusEvent) => { focused = event.target instanceof HTMLElement && dialog.contains(event.target) ? event.target : null; };
    // An asynchronous update can remove the restored trigger after a nested
    // modal closes. Contained dialogs use show(), so the browser will otherwise
    // leave focus on body, outside both the Tab cycle and Escape handler.
    const observer = new MutationObserver(() => {
      if (focused && (!focused.isConnected || focused.matches(':disabled') || !focused.getClientRects().length) && document.activeElement === document.body && dialog.open && !dialog.closest('[inert]') && dialog.getClientRects().length) titleRef.current?.focus({ preventScroll: true });
    });
    observer.observe(dialog, { childList: true, subtree: true, attributes: true, attributeFilter: ['disabled', 'hidden'] });
    document.addEventListener('focusin', focus);
    return () => { observer.disconnect(); document.removeEventListener('focusin', focus); };
  }, [open]);
  function outside(event: { clientX: number; clientY: number }) {
    const rect = ref.current?.getBoundingClientRect();
    return !!rect && (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom);
  }
  const dialog = <dialog key="panel" ref={ref} role={docked ? 'complementary' : undefined} data-docked={docked || undefined} className={`ui-dialog ui-dialog--${size}${drawer ? ' ui-drawer' : ''} ${className}`} data-contained={contained || undefined} data-side={drawer ? drawerSide : undefined} data-state={open ? 'open' : 'closed'} aria-modal={contained ? true : undefined} aria-labelledby={`${id}-title`} aria-describedby={description ? `${id}-description` : undefined}
    onClickCapture={event => { if (!open) { event.preventDefault(); event.stopPropagation(); } }}
    onSubmitCapture={event => { if (!open) { event.preventDefault(); event.stopPropagation(); } }}
    onKeyDownCapture={event => { if (!open) { event.preventDefault(); event.stopPropagation(); } }}
    onKeyDown={event => {
      if ((contained || docked) && event.key === 'Escape' && !event.defaultPrevented && (event.target as HTMLElement).closest('dialog') === event.currentTarget) {
        event.preventDefault(); event.stopPropagation();
        if (dismissible) onOpenChange(false);
        return;
      }
      if (docked || event.key !== 'Tab' || event.defaultPrevented || (event.target as HTMLElement).closest('dialog') !== event.currentTarget) return;
      const stops = getTabStops(event.currentTarget);
      const first = stops[0], last = stops.at(-1);
      if (!first) { event.preventDefault(); titleRef.current?.focus(); }
      else if (event.shiftKey && (document.activeElement === first || document.activeElement === titleRef.current)) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }}
    onCancel={event => { event.preventDefault(); event.stopPropagation(); if (dismissible) onOpenChange(false); }}
    onClose={event => { if (event.target === event.currentTarget && ref.current && !ref.current.open) { if (open) onOpenChange(false); reportClosed(); } }}
    // Top-layer popovers can extend beyond the dialog without being its backdrop.
    onPointerDown={event => { startedOutside.current = event.target === event.currentTarget && outside(event); }}
    onClick={event => {
      if ((event.target as Element).closest('dialog') !== event.currentTarget) return;
      if (!docked && dismissible && startedOutside.current && event.target === event.currentTarget && outside(event)) onOpenChange(false);
      startedOutside.current = false;
    }}>
    <ModalParentContext.Provider value={ref}><header className="ui-dialog__header"><div><h2 ref={titleRef} tabIndex={-1} id={`${id}-title`} className={headerContent ? 'ui-sr-only' : undefined}>{title}</h2>{description && <p id={`${id}-description`} className={headerContent ? 'ui-sr-only' : undefined}>{description}</p>}{headerContent}</div>
      {dismissible && showCloseButton && <button type="button" className="ui-icon-button" aria-label={closeLabel ?? messages.close} onClick={() => onOpenChange(false)}><UIIcon name="close" /></button>}
    </header>
    {Children.toArray(children).some(child => child !== '') && <div className="ui-dialog__body">{children}</div>}
    {footer && <footer className="ui-dialog__footer">{footer}</footer>}
    </ModalParentContext.Provider>
  </dialog>;
  const layer = <div ref={layerRef} className={contained ? `ui-modal-layer${scope?.presentation === 'dialog' ? ' ui-modal-layer--dialog' : ''}` : 'ui-modal-host'} hidden={contained && !open && !present}
    onPointerDown={event => { if (contained) startedOutside.current = event.target === backdropRef.current; }}
    onClick={event => {
      if (contained && event.target === backdropRef.current && startedOutside.current && dismissible) onOpenChange(false);
      startedOutside.current = false;
    }}>{contained && <div ref={backdropRef} className="ui-modal-backdrop" aria-hidden="true" />}{dialog}</div>;
  if (host) return <><span hidden ref={anchorRef} />{createPortal(layer, host)}</>;
  return contained ? createPortal(layer, scope!.container!) : dialog;
}

export function Dialog(props: DialogProps) { return <Modal {...props} />; }
export function Drawer({ side = 'right', ...props }: DialogProps & { side?: 'right' | 'bottom'; docked?: boolean }) { return <Modal {...props} drawer drawerSide={side} />; }
