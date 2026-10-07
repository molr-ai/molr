import { useLayoutEffect, useState } from 'react';
import { breakpoints } from '@molr/ui';

/** Measure the stable outer workspace, not the chat column that docking shrinks. */
export function usePanelDocking(element: HTMLElement | null) {
  const [docked, setDocked] = useState(false);
  useLayoutEffect(() => {
    if (!element) return;
    const update = () => setDocked(element.getBoundingClientRect().width > breakpoints.chatPanel);
    update();
    let frame = 0;
    const observer = new ResizeObserver(() => { cancelAnimationFrame(frame); frame = requestAnimationFrame(update); });
    observer.observe(element);
    return () => { observer.disconnect(); cancelAnimationFrame(frame); };
  }, [element]);
  return docked;
}
