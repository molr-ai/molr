import { useId } from 'react';
import type { ReactNode } from 'react';
import { useUILocale } from '../locale';
import { UIIcon } from './icons';
import { useChoiceScroll } from '../use-choice-scroll';

export type TabItem<Value extends string = string> = { value: Value; label: string; content: ReactNode; disabled?: boolean; icon?: ReactNode };
export type TabsProps<Value extends string = string> = {
  label: string; items: readonly TabItem<Value>[]; value: Value; onValueChange: (value: Value) => void;
  variant?: 'line' | 'segmented'; size?: 'sm' | 'md'; orientation?: 'horizontal' | 'vertical'; className?: string;
};
export function Tabs<Value extends string>({ label, items, value, onValueChange, variant = 'line', size = 'md', orientation = 'horizontal', className = '' }: TabsProps<Value>) {
  const id = useId();
  const enabled = items.flatMap((item, index) => item.disabled ? [] : [index]);
  const selected = items.findIndex(item => item.value === value && !item.disabled);
  const current = selected < 0 ? enabled[0] : selected;
  const listRef = useChoiceScroll(current, orientation === 'horizontal');
  return <div className={`ui-tabs ui-tabs--${variant} ui-tabs--${size} ui-tabs--${orientation} ${className}`}>
    <div ref={listRef} className="ui-tabs__list" role="tablist" aria-label={label} aria-orientation={orientation}>
      {items.map((item, index) => <button key={item.value} type="button" role="tab" id={`${id}-tab-${index}`} aria-controls={`${id}-panel-${index}`} aria-selected={index === current} tabIndex={index === current ? 0 : -1} disabled={item.disabled}
        onClick={() => onValueChange(item.value)} onKeyDown={event => {
          const previous = orientation === 'horizontal' ? 'ArrowLeft' : 'ArrowUp';
          const next = orientation === 'horizontal' ? 'ArrowRight' : 'ArrowDown';
          if (![previous, next, 'Home', 'End'].includes(event.key)) return;
          event.preventDefault();
          const position = enabled.indexOf(index);
          const destination = event.key === 'Home' ? enabled[0] : event.key === 'End' ? enabled.at(-1) : enabled[(position + (event.key === next ? 1 : -1) + enabled.length) % enabled.length];
          if (destination !== undefined) {
            onValueChange(items[destination].value);
            event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[destination]?.focus({ preventScroll: true });
          }
        }}>{item.icon && <span aria-hidden="true">{item.icon}</span>}{item.label}</button>)}
    </div>
    {items.map((item, index) => <div key={item.value} className="ui-tabs__panel" id={`${id}-panel-${index}`} role="tabpanel" aria-labelledby={`${id}-tab-${index}`} hidden={index !== current} tabIndex={0}>{item.content}</div>)}
  </div>;
}

export type BreadcrumbItem = { label: string; href?: string };
export function Breadcrumb({ items, label, className = '' }: { items: readonly BreadcrumbItem[]; label?: string; className?: string }) {
  const { messages } = useUILocale();
  return <nav className={`ui-breadcrumb ${className}`} aria-label={label ?? messages.breadcrumb}><ol>
    {items.map((item, index) => <li key={`${item.label}-${index}`}>
      {index > 0 && <UIIcon name="chevron-right" />}
      {item.href && index < items.length - 1 ? <a href={item.href}>{item.label}</a> : <span aria-current={index === items.length - 1 ? 'page' : undefined}>{item.label}</span>}
    </li>)}
  </ol></nav>;
}

export type PaginationProps = { page: number; totalPages: number; onPageChange: (page: number) => void; disabled?: boolean; label?: string; className?: string };
export function Pagination({ page, totalPages, onPageChange, disabled, label, className = '' }: PaginationProps) {
  const { messages } = useUILocale();
  const total = Math.max(1, Number.isFinite(totalPages) ? Math.floor(totalPages) : 1);
  const current = Math.max(1, Math.min(total, Number.isFinite(page) ? Math.floor(page) : 1));
  const visible = [...new Set([1, total, current - 1, current, current + 1].filter(number => number >= 1 && number <= total))].sort((a, b) => a - b);
  return <nav className={`ui-pagination ${className}`} aria-label={label ?? messages.pagination}>
    <button type="button" className="ui-icon-button" aria-label={messages.previous} disabled={disabled || current === 1} onClick={() => onPageChange(current - 1)}><UIIcon name="chevron-left" /></button>
    <div className="ui-pagination__pages">{visible.map((number, index) => <span key={number}>
      {index > 0 && number - visible[index - 1] > 1 && <span className="ui-pagination__gap" aria-hidden="true">…</span>}
      <button type="button" disabled={disabled} aria-label={messages.page(number)} aria-current={number === current ? 'page' : undefined} onClick={() => onPageChange(number)}>{number}</button>
    </span>)}</div>
    <span className="ui-pagination__summary">{messages.pageOf(current, total)}</span>
    <button type="button" className="ui-icon-button" aria-label={messages.next} disabled={disabled || current === total} onClick={() => onPageChange(current + 1)}><UIIcon name="chevron-right" /></button>
  </nav>;
}
