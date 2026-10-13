import { useLayoutEffect } from 'react';
import './scrollbars.css';

type Axis = 'x' | 'y';
type Scrollbar = { layer: HTMLDivElement; thumbs: HTMLDivElement[]; timer?: ReturnType<typeof setTimeout>; fade?: Animation; dragging: boolean };

// Native scrolling remains in charge; only its visual controls are overlaid.
export function Scrollbars() {
  useLayoutEffect(() => {
    const active = new Map<HTMLElement, Scrollbar>();
    let frame = 0;
    const root = document.documentElement;
    root.dataset.overlayScrollbars = '';
    function remove(element: HTMLElement) {
      const bar = active.get(element);
      if (!bar) return;
      clearTimeout(bar.timer);
      bar.fade?.cancel();
      bar.layer.remove();
      delete element.dataset.scrolling;
      active.delete(element);
    }
    function keepVisible(bar: Scrollbar) {
      clearTimeout(bar.timer);
      bar.fade?.cancel();
      bar.fade = undefined;
    }
    function hideLater(element: HTMLElement, bar: Scrollbar) {
      clearTimeout(bar.timer);
      if (!bar.dragging) bar.timer = setTimeout(() => {
        if (root.dataset.reducedMotion === 'true' || window.matchMedia('(prefers-reduced-motion: reduce)').matches) { remove(element); return; }
        bar.fade = bar.layer.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 240, easing: 'linear', fill: 'forwards' });
        bar.fade.onfinish = () => remove(element);
      }, 1000);
    }
    function geometry(element: HTMLElement, axis: Axis) {
      const viewport = element === document.scrollingElement;
      const rect = element.getBoundingClientRect();
      const scaleX = viewport ? 1 : rect.width / (element.offsetWidth || 1);
      const scaleY = viewport ? 1 : rect.height / (element.offsetHeight || 1);
      const left = viewport ? 0 : rect.left + element.clientLeft * scaleX;
      const top = viewport ? 0 : rect.top + element.clientTop * scaleY;
      const width = viewport ? root.clientWidth : element.clientWidth * scaleX;
      const height = viewport ? window.innerHeight : element.clientHeight * scaleY;
      let clipLeft = Math.max(0, left), clipTop = Math.max(0, top), clipRight = Math.min(window.innerWidth, left + width), clipBottom = Math.min(window.innerHeight, top + height);
      for (let parent = element.parentElement; parent && parent !== document.body; parent = parent.parentElement) {
        const style = getComputedStyle(parent), bounds = parent.getBoundingClientRect();
        if (style.overflowX !== 'visible') { clipLeft = Math.max(clipLeft, bounds.left); clipRight = Math.min(clipRight, bounds.right); }
        if (style.overflowY !== 'visible') { clipTop = Math.max(clipTop, bounds.top); clipBottom = Math.min(clipBottom, bounds.bottom); }
      }
      const horizontal = axis === 'x', client = horizontal ? element.clientWidth : element.clientHeight;
      const extent = horizontal ? element.scrollWidth : element.scrollHeight, range = extent - client;
      const track = Math.max(0, (horizontal ? width : height) - 8);
      const size = Math.min(track, Math.max(24, track * client / extent));
      const offset = horizontal ? Math.abs(element.scrollLeft) : element.scrollTop;
      const rtl = horizontal && getComputedStyle(element).direction === 'rtl';
      const progress = range > 0 ? Math.min(1, Math.max(0, offset / range)) : 0;
      const start = 4 + (track - size) * (rtl ? 1 - progress : progress);
      return { left: horizontal ? left + start : left + width - 10, top: horizontal ? top + height - 10 : top + start, width: horizontal ? size : 8, height: horizontal ? 8 : size, range, travel: track - size, clipLeft, clipTop, clipRight, clipBottom };
    }
    function draw() {
      frame = 0;
      for (const [element, bar] of active) {
        if (!element.isConnected || !element.getClientRects().length || element.closest('dialog:not([open]), [hidden]')) { remove(element); continue; }
        bar.thumbs.forEach((thumb, index) => {
          const axis = index === 0 ? 'y' : 'x', g = geometry(element, axis);
          const style = getComputedStyle(element), overflow = axis === 'x' ? style.overflowX : style.overflowY;
          const visible = g.range > 1 && g.travel > 0 && (element === document.scrollingElement || /auto|scroll|overlay/.test(overflow));
          const left = Math.max(g.left, g.clipLeft), top = Math.max(g.top, g.clipTop), right = Math.min(g.left + g.width, g.clipRight), bottom = Math.min(g.top + g.height, g.clipBottom);
          thumb.hidden = !visible || right <= left || bottom <= top;
          Object.assign(thumb.style, { left: `${left}px`, top: `${top}px`, width: `${Math.max(0, right - left)}px`, height: `${Math.max(0, bottom - top)}px` });
        });
      }
      if (active.size) frame = requestAnimationFrame(draw);
    }
    function show(element: HTMLElement) {
      let bar = active.get(element);
      if (!bar) {
        const layer = document.createElement('div');
        layer.className = 'webui-scrollbars';
        layer.popover = 'manual';
        layer.setAttribute('aria-hidden', 'true');
        // Keep controls inside their modal/popover for native inertness and focus containment.
        (element.closest('dialog[open], [popover]') ?? document.body).append(layer);
        const current: Scrollbar = { layer, thumbs: [], dragging: false };
        for (const axis of ['y', 'x'] as const) {
          const thumb = document.createElement('div');
          thumb.className = 'webui-scrollbar-thumb';
          thumb.dataset.axis = axis;
          thumb.onpointerdown = event => {
            if (event.button !== 0) return;
            event.preventDefault(); event.stopPropagation();
            const g = geometry(element, axis), start = axis === 'x' ? event.clientX : event.clientY, initial = axis === 'x' ? element.scrollLeft : element.scrollTop;
            current.dragging = true; keepVisible(current);
            thumb.setPointerCapture(event.pointerId);
            thumb.onpointermove = move => {
              const delta = ((axis === 'x' ? move.clientX : move.clientY) - start) * g.range / Math.max(1, g.travel);
              element.scrollTo({ [axis === 'x' ? 'left' : 'top']: initial + delta, behavior: 'instant' });
            };
          };
          thumb.onlostpointercapture = () => { current.dragging = false; thumb.onpointermove = null; hideLater(element, current); };
          thumb.onpointerenter = () => keepVisible(current);
          thumb.onpointerleave = () => hideLater(element, current);
          layer.append(thumb); current.thumbs.push(thumb);
        }
        layer.showPopover();
        bar = current; active.set(element, bar);
      }
      keepVisible(bar);
      element.dataset.scrolling = '';
      hideLater(element, bar);
      if (!frame) frame = requestAnimationFrame(draw);
    }
    function onScroll(event: Event) {
      const element = event.target === document ? document.scrollingElement : event.target;
      if (element instanceof HTMLElement) show(element);
    }
    document.addEventListener('scroll', onScroll, { capture: true, passive: true });
    return () => {
      document.removeEventListener('scroll', onScroll, true);
      cancelAnimationFrame(frame);
      for (const element of active.keys()) remove(element);
      delete root.dataset.overlayScrollbars;
    };
  }, []);
  return null;
}
