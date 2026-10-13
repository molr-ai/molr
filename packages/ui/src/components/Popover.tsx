import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useUILocale } from '../locale';
import { triggerElement, useComposedRefs, useFloating } from './floating';
import type { FloatingSide, FloatingTrigger } from './floating';
import { UIIcon } from './icons';
import { Dialog } from './Dialog';
import { useModalScope } from '../modal-scope';
import { subscribeTooltipActivity, tooltipDocumentActive, tooltipInputVersion } from './tooltip-activity';

export type PopoverProps = { trigger: FloatingTrigger; title: string; children: ReactNode; align?: 'start' | 'end'; maxHeight?: number; width?: number; open?: boolean; onOpenChange?: (open: boolean) => void };
export function Popover(props: PopoverProps) {
  const scope = useModalScope();
  return scope ? <ContainedPopover {...props} /> : <FloatingPopover {...props} />;
}

function ContainedPopover({ trigger, title, children, open: controlled, onOpenChange }: PopoverProps) {
  const [internal, setInternal] = useState(false);
  const open = controlled ?? internal;
  const change = (value: boolean) => { setInternal(value); onOpenChange?.(value); };
  const anchor = useRef<HTMLElement>(null);
  const triggerRef = useComposedRefs(anchor, trigger.props.ref);
  return <>
    {triggerElement(trigger, triggerRef, { 'aria-haspopup': 'dialog', 'aria-expanded': open, onClick: event => { trigger.props.onClick?.(event); if (!event.defaultPrevented) change(!open); } })}
    <Dialog open={open} onOpenChange={change} title={title} size="sm">{children}</Dialog>
  </>;
}

function FloatingPopover({ trigger, title, children, align, maxHeight, width = 320, open: controlled, onOpenChange }: PopoverProps) {
  const id = useId();
  const [internal, setInternal] = useState(false);
  const open = controlled ?? internal;
  const change = (value: boolean) => { setInternal(value); onOpenChange?.(value); };
  const { anchorRef, panelRef, style, side } = useFloating(open, change, { align, width, maxHeight });
  const triggerRef = useComposedRefs(anchorRef, trigger.props.ref);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const { messages } = useUILocale();
  useLayoutEffect(() => { if (open) titleRef.current?.focus({ preventScroll: true }); }, [open]);
  function close() { change(false); anchorRef.current?.focus({ preventScroll: true }); }
  return <>
    {triggerElement(trigger, triggerRef, { 'aria-haspopup': 'dialog', 'aria-expanded': open, 'aria-controls': id, onClick: event => { trigger.props.onClick?.(event); if (!event.defaultPrevented) open ? close() : change(true); } })}
    <div id={id} ref={panelRef} popover="auto" role="dialog" aria-labelledby={`${id}-title`} className="ui-floating ui-popover" style={style} data-side={side}
      onBlur={event => { if (event.relatedTarget instanceof Node && !event.currentTarget.contains(event.relatedTarget) && !anchorRef.current?.contains(event.relatedTarget)) change(false); }}
      onToggle={event => { if (event.newState === 'closed' && !event.currentTarget.matches(':popover-open')) change(false); }}
      onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); } }}>
      <div className="ui-popover__header"><h3 id={`${id}-title`} ref={titleRef} tabIndex={-1}>{title}</h3><button type="button" className="ui-icon-button" aria-label={messages.close} onClick={close}><UIIcon name="close" /></button></div>
      {children}
    </div>
  </>;
}

export type TooltipProps = { children: FloatingTrigger; content: ReactNode; delay?: number; side?: FloatingSide; width?: number | 'auto'; openOnClick?: boolean };
export function Tooltip({ children, content, delay = 400, side: preferredSide = 'top', width = 220, openOnClick = false }: TooltipProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const focused = useRef(false);
  const hovered = useRef(false);
  const pinned = useRef(false);
  const suspendedAt = useRef<number | null>(null);
  const awaitingInteraction = () => suspendedAt.current === tooltipInputVersion();
  const { anchorRef, panelRef, style, side } = useFloating(open, setOpen, { width, preferredSide });
  const triggerRef = useComposedRefs(anchorRef, children.props.ref);
  function cancel() { if (timer.current) clearTimeout(timer.current); timer.current = null; }
  const active = tooltipDocumentActive;
  function show() {
    cancel();
    if (awaitingInteraction() || !active()) return;
    timer.current = setTimeout(() => { if (!awaitingInteraction() && active()) setOpen(true); }, delay);
  }
  function hide() { cancel(); pinned.current = false; setOpen(false); }
  function hideWhenIdle() {
    if (!focused.current && !hovered.current && !pinned.current) {
      cancel();
      timer.current = setTimeout(() => setOpen(false), 120);
    }
  }
  useEffect(() => {
    // Window or dialog restoration can focus the old trigger again. Wait for fresh input,
    // rather than a timeout or relatedTarget (also null for ordinary focus).
    const suspend = () => { suspendedAt.current = tooltipInputVersion(); focused.current = false; hovered.current = false; hide(); };
    const unsubscribe = subscribeTooltipActivity(suspend);
    return () => { cancel(); unsubscribe(); };
  }, []);
  useEffect(() => {
    if (!open) return;
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      if (event.target instanceof Node && anchorRef.current?.contains(event.target)) {
        event.preventDefault();
        event.stopPropagation();
      }
      hide();
    };
    // Dismiss every visible tooltip, even when the focused trigger stops Escape.
    document.addEventListener('keydown', escape, true);
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && !anchorRef.current?.contains(event.target) && !panelRef.current?.contains(event.target)) hide();
    };
    if (openOnClick) document.addEventListener('pointerdown', outside, true);
    return () => {
      document.removeEventListener('keydown', escape, true);
      document.removeEventListener('pointerdown', outside, true);
    };
  }, [open, openOnClick]);
  return <>
    {triggerElement(children, triggerRef, {
      'aria-describedby': [children.props['aria-describedby'], id].filter(Boolean).join(' '),
      onPointerEnter: event => { children.props.onPointerEnter?.(event); if (event.pointerType !== 'touch' && !awaitingInteraction()) { hovered.current = true; show(); } },
      onPointerMove: event => {
        children.props.onPointerMove?.(event);
        if (event.pointerType !== 'touch' && awaitingInteraction() && active()) {
          suspendedAt.current = null; hovered.current = true; show();
        }
      },
      onPointerLeave: event => { children.props.onPointerLeave?.(event); hovered.current = false; hideWhenIdle(); },
      onFocus: event => { children.props.onFocus?.(event); focused.current = true; show(); },
      onBlur: event => { children.props.onBlur?.(event); focused.current = false; pinned.current = false; hideWhenIdle(); },
      onClick: event => {
        children.props.onClick?.(event);
        if (event.defaultPrevented) return;
        if (!openOnClick || pinned.current) hide();
        else { cancel(); pinned.current = true; setOpen(true); }
      },
      onKeyDown: event => {
        children.props.onKeyDown?.(event);
        if (event.key === 'Escape' && !event.defaultPrevented) {
          if (open) { event.preventDefault(); event.stopPropagation(); }
          hide();
        }
      },
    })}
    <div id={id} ref={panelRef} popover="manual" role="tooltip" className="ui-floating ui-tooltip" style={style} data-side={side} onPointerEnter={event => { if (event.pointerType !== 'touch') { hovered.current = true; cancel(); } }} onPointerLeave={() => { hovered.current = false; hideWhenIdle(); }}>{content}</div>
  </>;
}
