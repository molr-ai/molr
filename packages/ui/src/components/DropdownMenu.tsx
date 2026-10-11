import { useId, useLayoutEffect, useRef, useState } from 'react';
import { UIIcon } from './icons';
import type { ReactNode } from 'react';
import { triggerElement, useComposedRefs, useFloating } from './floating';
import type { FloatingTrigger } from './floating';

export type MenuItem = { id: string; label: string; onSelect: () => void; icon?: ReactNode; shortcut?: string; disabled?: boolean; selected?: boolean; danger?: boolean; separatorBefore?: boolean };
export type DropdownMenuProps = { trigger: FloatingTrigger; label: string; items: readonly MenuItem[]; align?: 'start' | 'end' };

export function DropdownMenu({ trigger, label, items, align = 'start' }: DropdownMenuProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const search = useRef({ text: '', time: 0 });
  const { anchorRef, panelRef, style, side } = useFloating(open, setOpen, { align, width: 252 });
  const triggerRef = useComposedRefs(anchorRef, trigger.props.ref);
  const enabled = items.flatMap((item, index) => item.disabled ? [] : [index]);
  function show(last = false) { setActive(last ? enabled.at(-1) ?? -1 : enabled[0] ?? -1); setOpen(true); }
  function close(restore = true) { setOpen(false); if (restore) anchorRef.current?.focus({ preventScroll: true }); }
  useLayoutEffect(() => {
    if (open) {
      if (!items[active] || items[active].disabled) setActive(enabled[0] ?? -1);
      else panelRef.current?.querySelector<HTMLElement>(`[data-menu-index="${active}"]`)?.focus({ preventScroll: true });
    }
  }, [open, active, items, enabled.join(','), panelRef]);
  return <>
    {triggerElement(trigger, triggerRef, {
      'aria-haspopup': 'menu', 'aria-expanded': open, 'aria-controls': id,
      onClick: event => { trigger.props.onClick?.(event); if (!event.defaultPrevented) open ? close() : show(); },
      onKeyDown: event => {
        trigger.props.onKeyDown?.(event);
        if (!event.defaultPrevented && ['ArrowDown', 'ArrowUp'].includes(event.key)) { event.preventDefault(); show(event.key === 'ArrowUp'); }
      },
    })}
    <div id={id} ref={panelRef} popover="auto" role="menu" aria-label={label} className="ui-floating ui-menu" style={style} data-side={side}
      onToggle={event => { if (event.newState === 'closed' && !event.currentTarget.matches(':popover-open')) setOpen(false); }}
      onKeyDown={event => {
        if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); return; }
        if (event.key === 'Tab') { close(); return; }
        const index = enabled.indexOf(active);
        if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
          event.preventDefault();
          const next = event.key === 'Home' ? enabled[0] : event.key === 'End' ? enabled.at(-1) : enabled[(index + (event.key === 'ArrowDown' ? 1 : -1) + enabled.length) % enabled.length];
          if (next !== undefined) setActive(next);
        } else if (event.key.length === 1 && event.key !== ' ' && !event.ctrlKey && !event.metaKey && !event.altKey && !event.nativeEvent.isComposing) {
          const now = performance.now();
          const text = now - search.current.time < 700 ? search.current.text + event.key.toLowerCase() : event.key.toLowerCase();
          const query = [...text].every(char => char === text[0]) ? text[0] : text;
          const ordered = [...enabled.slice(index + 1), ...enabled.slice(0, index + 1)];
          const match = ordered.find(i => items[i].label.toLowerCase().startsWith(query));
          if (match !== undefined) { event.preventDefault(); setActive(match); }
          search.current = { text, time: now };
        }
      }}>
      {items.map((item, index) => <div role="none" key={item.id}>
        {item.separatorBefore && <div role="separator" className="ui-menu__separator" />}
        <button type="button" role={item.selected === undefined ? "menuitem" : "menuitemradio"} aria-checked={item.selected} data-menu-index={index} tabIndex={open && index === active ? 0 : -1} aria-disabled={item.disabled || undefined} className={`ui-menu__item${item.danger ? ' ui-menu__item--danger' : ''}`}
          onFocus={() => { if (!item.disabled) setActive(index); }} onClick={() => { if (!item.disabled) { close(); item.onSelect(); } }}>
          {item.icon && <span aria-hidden="true">{item.icon}</span>}<span>{item.label}</span>{item.shortcut && <kbd>{item.shortcut}</kbd>}{item.selected && <UIIcon name="check" className="ui-menu__check" />}
        </button>
      </div>)}
    </div>
  </>;
}
