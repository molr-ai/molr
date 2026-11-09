import { useLayoutEffect, useRef, type HTMLAttributes } from 'react';
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

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const items = [...container.querySelectorAll<HTMLElement>('[data-sliding-item]')];
    if (disabled) {
      delete container.dataset.highlight;
      return;
    }
    const target = items.find(item => item.dataset.slidingItem === value);
    const measure = () => {
      if (!target || !target.getClientRects().length) {
        delete container.dataset.highlight;
        return;
      }
      const visible = container.hasAttribute('data-highlight');
      container.toggleAttribute('data-highlight-moving', visible);
      const bounds = container.getBoundingClientRect(), rect = target.getBoundingClientRect();
      const x = rect.left - bounds.left + container.scrollLeft - container.clientLeft;
      const y = rect.top - bounds.top + container.scrollTop - container.clientTop;
      container.style.setProperty('--sliding-highlight-x', `${x}px`);
      container.style.setProperty('--sliding-highlight-y', `${y}px`);
      container.style.setProperty('--sliding-highlight-width', `${rect.width}px`);
      container.style.setProperty('--sliding-highlight-height', `${rect.height}px`);
      // Establish the new position while invisible, then fade in without an entrance slide.
      if (!visible) getComputedStyle(container, '::before').transform;
      container.dataset.highlight = '';
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(container);
    items.forEach(item => observer.observe(item));
    return () => observer.disconnect();
  }, [children, value, disabled, reducedMotion]);

  return <Tag {...props} ref={(node: HTMLElement | null) => { containerRef.current = node; }} className={`ui-sliding-highlight ${className}`} data-reduced-motion={reducedMotion || undefined}>{children}</Tag>;
}
