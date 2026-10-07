import { useLayoutEffect, useRef } from 'react';
import type { RefObject } from 'react';
import { getOverlayViewport, useModalScope } from '../modal-scope';
import { useUILocale } from '../locale';
import type { ChatComposerCommand } from './chat';

/** A non-modal suggestion list: typing focus stays in the composer. */
export function ComposerCommandMenu({ id, anchor, commands, selected, onSelect, onActive, onDismiss }: {
  id: string; anchor: RefObject<HTMLTextAreaElement | null>; commands: readonly ChatComposerCommand[]; selected: number;
  onSelect: (index: number) => void; onActive: (index: number) => void; onDismiss: () => void;
}) {
  const { messages } = useUILocale();
  const boundary = useModalScope()?.container;
  const popup = useRef<HTMLDivElement>(null), results = useRef<HTMLDivElement>(null);
  const dismiss = useRef(onDismiss); dismiss.current = onDismiss;
  useLayoutEffect(() => {
    const menu = popup.current, input = anchor.current, composer = input?.closest('form');
    if (!menu || !input || !composer) return;
    menu.showPopover();
    function position() {
      if (!menu || !composer || !input) return;
      const rect = composer.getBoundingClientRect(), view = getOverlayViewport(boundary);
      if (!input.getClientRects().length || input.closest('[inert], [aria-hidden="true"]') || rect.bottom < view.top || rect.top > view.top + view.height) { dismiss.current(); return; }
      const gap = 6, margin = 8;
      menu.style.width = `${Math.max(0, Math.min(360, rect.width, view.width - margin * 2))}px`;
      const above = Math.max(0, rect.top - view.top - gap - margin), below = Math.max(0, view.top + view.height - rect.bottom - gap - margin);
      const top = above >= Math.min(menu.scrollHeight, 160) || above >= below;
      menu.style.maxHeight = `${Math.min(320, top ? above : below)}px`;
      menu.style.left = `${Math.max(view.left + margin, Math.min(rect.left, view.left + view.width - menu.offsetWidth - margin))}px`;
      menu.style.top = `${top ? rect.top - gap - menu.getBoundingClientRect().height : rect.bottom + gap}px`;
    }
    const outside = (event: PointerEvent) => { if (event.target instanceof Node && !menu.contains(event.target) && !composer.contains(event.target)) dismiss.current(); };
    const observer = new ResizeObserver(position); observer.observe(composer); observer.observe(menu); if (boundary) observer.observe(boundary);
    window.addEventListener('resize', position); window.addEventListener('scroll', position, true);
    window.visualViewport?.addEventListener('resize', position); window.visualViewport?.addEventListener('scroll', position);
    document.addEventListener('pointerdown', outside); position();
    return () => {
      observer.disconnect(); window.removeEventListener('resize', position); window.removeEventListener('scroll', position, true);
      window.visualViewport?.removeEventListener('resize', position); window.visualViewport?.removeEventListener('scroll', position);
      document.removeEventListener('pointerdown', outside);
      if (menu.matches(':popover-open')) menu.hidePopover();
    };
  }, [anchor, boundary]);
  useLayoutEffect(() => {
    const list = results.current, option = document.getElementById(`${id}-command-${selected}`);
    if (!list || !option) return;
    const bounds = option.getBoundingClientRect(), viewport = list.getBoundingClientRect();
    if (bounds.top < viewport.top) list.scrollTop -= viewport.top - bounds.top;
    else if (bounds.bottom > viewport.bottom) list.scrollTop += bounds.bottom - viewport.bottom;
  }, [id, selected, commands]);
  return <div ref={popup} popover="manual" className="composer-commands select__popup" onMouseDown={event => event.preventDefault()}>
    <div className="composer-commands__heading">{messages.chat.commands}</div>
    <div ref={results} id={`${id}-commands`} role="listbox" aria-label={messages.chat.commands} className="composer-commands__results">
      {commands.map((command, index) => <button key={command.name} tabIndex={-1} id={`${id}-command-${index}`} type="button" role="option" aria-selected={index === selected} data-active={index === selected || undefined} aria-disabled={command.disabled || undefined} className="composer-command select__option" onPointerMove={() => onActive(index)} onClick={() => onSelect(index)}>
        <span className="composer-command__symbol" aria-hidden="true">/</span>
        <span className="select__option-text"><span className="select__option-label">/{command.name}</span><span className="composer-command__description">{command.description}</span></span>
        {index === selected && !command.disabled && <span className="composer-command__enter" aria-hidden="true">↵</span>}
      </button>)}
      {!commands.length && <p className="composer-commands__empty" role="status">{messages.chat.noCommands}</p>}
    </div>
    <div className="composer-commands__footer" aria-hidden="true"><span><kbd>↑</kbd><kbd>↓</kbd> {messages.chat.commandChoose}</span><span><kbd>Tab</kbd> {messages.chat.commandComplete}</span><span><kbd>Esc</kbd> {messages.close}</span></div>
  </div>;
}
