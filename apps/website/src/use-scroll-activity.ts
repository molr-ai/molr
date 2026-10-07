import { useCallback, type RefCallback } from 'react';

// Toggle the native scrollbar without rerendering the document while scrolling.
export function useScrollActivity(trackOverflow = false): RefCallback<HTMLElement> {
  return useCallback(node => {
    if (!node) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const updateOverflow = () => {
      if (trackOverflow) node.classList.toggle('has-more-below', node.clientHeight > 0 && node.scrollHeight - node.clientHeight - node.scrollTop > 1);
    };
    const onScroll = () => {
      updateOverflow();
      node.classList.add('is-scrolling');
      clearTimeout(timer);
      timer = setTimeout(() => node.classList.remove('is-scrolling'), 700);
    };
    node.addEventListener('scroll', onScroll, { passive: true });
    const resize = trackOverflow ? new ResizeObserver(updateOverflow) : undefined;
    const observeContent = () => {
      resize?.disconnect();
      resize?.observe(node);
      for (const child of node.children) resize?.observe(child);
      updateOverflow();
    };
    const mutations = trackOverflow ? new MutationObserver(observeContent) : undefined;
    mutations?.observe(node, { childList: true, subtree: true, characterData: true });
    observeContent();
    return () => {
      node.removeEventListener('scroll', onScroll);
      clearTimeout(timer);
      resize?.disconnect();
      mutations?.disconnect();
      node.classList.remove('is-scrolling', 'has-more-below');
    };
  }, [trackOverflow]);
}
