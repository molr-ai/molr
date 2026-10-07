import { useUILocale } from './locale';
import { useLayoutEffect, useState } from 'react';

/** Keep a selected horizontal choice visible without scrolling the page. */
export function useChoiceScroll(selection: string | number | undefined, enabled = true) {
  const { reducedMotion } = useUILocale();
  const [list, setList] = useState<HTMLDivElement | null>(null);

  useLayoutEffect(() => {
    if (!list || !enabled) return;
    let frame = 0;

    function move(left: number) {
      list?.scrollTo({ left, behavior: reducedMotion || window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' });
    }

    function reveal() {
      if (!list || !list.clientWidth || list.scrollWidth <= list.clientWidth) return;
      const style = getComputedStyle(list);
      if (style.display !== 'flex' || style.flexDirection !== 'row' || style.flexWrap !== 'nowrap') return;

      const choices = Array.from(list.children).filter((child): child is HTMLElement => child instanceof HTMLElement && child.getClientRects().length > 0);
      const index = choices.findIndex(choice => choice.matches('[aria-selected="true"], [aria-pressed="true"], [aria-current="page"]') || choice.querySelector('input:checked, [aria-pressed="true"]'));
      const selected = choices[index];
      if (!selected) return;

      const maxScroll = list.scrollWidth - list.clientWidth;
      if (index === 0) { move(0); return; }
      if (index === choices.length - 1) { move(maxScroll); return; }

      const viewport = list.getBoundingClientRect();
      const left = viewport.left + list.clientLeft;
      const right = left + list.clientWidth;
      const bounds = selected.getBoundingClientRect();
      if (bounds.left >= left - 1 && bounds.right <= right + 1) return;

      const start = list.scrollLeft + bounds.left - left;
      const end = list.scrollLeft + bounds.right - left;
      const adjacent = choices[index + (bounds.left < left ? -1 : 1)];
      if (!adjacent) return;
      const neighbor = adjacent.getBoundingClientRect();
      const peek = Math.min(12, neighbor.width);
      const target = bounds.left < left
        ? list.scrollLeft + neighbor.right - left - peek
        : list.scrollLeft + neighbor.left - right + peek;
      // The selected item takes priority when space cannot fit a neighbor too.
      const position = bounds.width > list.clientWidth ? start : Math.max(end - list.clientWidth, Math.min(start, target));
      move(Math.max(0, Math.min(maxScroll, position)));
    }

    function schedule() {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(reveal);
    }

    reveal();
    const observer = new ResizeObserver(schedule);
    observer.observe(list);
    for (const child of list.children) observer.observe(child);
    // A click on the already selected item should also reveal it after a swipe.
    list.addEventListener('click', schedule);
    list.addEventListener('change', schedule);
    return () => {
      cancelAnimationFrame(frame);
      // A new selection must not inherit the previous selection's destination.
      list.scrollTo({ left: list.scrollLeft, behavior: 'instant' });
      observer.disconnect();
      list.removeEventListener('click', schedule);
      list.removeEventListener('change', schedule);
    };
  }, [list, selection, enabled, reducedMotion]);

  return setList;
}
