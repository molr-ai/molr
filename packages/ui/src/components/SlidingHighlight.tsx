import { useLayoutEffect, useRef, useState, type HTMLAttributes } from 'react';
import { useUILocale } from '../locale';

export type SlidingHighlightProps = Pick<HTMLAttributes<HTMLElement>, 'children' | 'className' | 'aria-label' | 'hidden' | 'style' | 'id'> & {
  as?: 'div' | 'nav' | 'ul';
  value?: string;
  disabled?: boolean;
};

/** Mark targets with data-sliding-item; selection and accessible semantics stay with the caller. */
export function SlidingHighlight({ as: Tag = 'div', value, disabled = false, children, className = '', ...props }: SlidingHighlightProps) {
  const containerRef = useRef<HTMLElement | null>(null);
  const { reducedMotion } = useUILocale();
  const [hovered, setHovered] = useState<string>();

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    if (disabled) {
      delete container.dataset.highlight;
      setHovered(undefined);
      return;
    }
    const items = [...container.querySelectorAll<HTMLElement>('[data-sliding-item]')];
    const target = items.find(item => item.dataset.slidingItem === hovered) ?? items.find(item => item.dataset.slidingItem === value);
    const measure = () => {
      if (!target || !target.getClientRects().length) {
        delete container.dataset.highlight;
        return;
      }
      const bounds = container.getBoundingClientRect(), rect = target.getBoundingClientRect();
      container.style.setProperty('--sliding-highlight-x', `${rect.left - bounds.left + container.scrollLeft - container.clientLeft}px`);
      container.style.setProperty('--sliding-highlight-y', `${rect.top - bounds.top + container.scrollTop - container.clientTop}px`);
      container.style.setProperty('--sliding-highlight-width', `${rect.width}px`);
      container.style.setProperty('--sliding-highlight-height', `${rect.height}px`);
      container.dataset.highlight = '';
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(container);
    items.forEach(item => observer.observe(item));
    return () => observer.disconnect();
  }, [children, value, hovered, disabled]);

  return <Tag {...props} ref={(node: HTMLElement | null) => { containerRef.current = node; }} className={`ui-sliding-highlight ${className}`} data-reduced-motion={reducedMotion || undefined}
    onPointerOver={event => {
      if (disabled || event.pointerType === 'touch') return;
      const item = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-sliding-item]') : null;
      setHovered(item?.dataset.slidingItem);
    }}
    onPointerOut={event => {
      const next = event.relatedTarget;
      if (!(next instanceof Element) || !event.currentTarget.contains(next) || !next.closest('[data-sliding-item]')) setHovered(undefined);
    }}
    onPointerLeave={() => setHovered(undefined)} onPointerCancel={() => setHovered(undefined)}>{children}</Tag>;
}
