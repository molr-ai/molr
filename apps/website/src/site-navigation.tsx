import { startTransition, useEffect, useLayoutEffect, useState, type ReactNode } from 'react';

type Position = [number, number];
interface Navigation {
  pathname: string;
  hash: string;
  revision: number;
  action: 'initial' | 'push' | 'pop';
  position?: Position;
}

const scrollKey = 'molrWebsiteScroll';

export function useSiteNavigation(enabled: boolean) {
  const [navigation, setNavigation] = useState<Navigation>(() => ({
    pathname: window.location.pathname,
    hash: window.location.hash,
    revision: 0,
    action: 'initial',
  }));

  useEffect(() => {
    if (!enabled) return;
    const previousRestoration = history.scrollRestoration;
    history.scrollRestoration = 'manual';
    let frame = 0;
    const savePosition = () => {
      cancelAnimationFrame(frame);
      history.replaceState({ ...history.state, [scrollKey]: [window.scrollX, window.scrollY] }, '');
    };
    const onScroll = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(savePosition);
    };
    const update = (action: Navigation['action'], position?: Position) => {
      const { pathname, hash } = window.location;
      startTransition(() => setNavigation(current => ({
        pathname,
        hash,
        revision: current.revision + 1,
        action,
        position,
      })));
    };
    const onClick = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const anchor = event.target instanceof Element ? event.target.closest('a[href]') : null;
      if (!(anchor instanceof HTMLAnchorElement) || anchor.hasAttribute('download') ||
        (anchor.target && anchor.target !== '_self') || anchor.relList.contains('external')) return;
      const url = new URL(anchor.href);
      if (url.origin !== window.location.origin || (url.pathname !== '/' && !/^\/(?:dev-)?docs(?:\/|$)/.test(url.pathname))) return;
      event.preventDefault();
      savePosition();
      if (url.href !== window.location.href) history.pushState(null, '', url);
      update('push');
    };
    const onPopState = (event: PopStateEvent) => {
      // A pending scroll event belongs to the outgoing page, not this entry.
      cancelAnimationFrame(frame);
      const position: unknown = event.state?.[scrollKey];
      update('pop', Array.isArray(position) && position.length === 2 && position.every(Number.isFinite) ? position as Position : undefined);
    };
    document.addEventListener('click', onClick);
    window.addEventListener('popstate', onPopState);
    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('pagehide', savePosition);
    return () => {
      cancelAnimationFrame(frame);
      history.scrollRestoration = previousRestoration;
      document.removeEventListener('click', onClick);
      window.removeEventListener('popstate', onPopState);
      window.removeEventListener('scroll', onScroll);
      window.removeEventListener('pagehide', savePosition);
    };
  }, [enabled]);

  return navigation;
}

// Keep a long homepage jump brief, regardless of the distance to installation.
function scrollToInstallation(target: HTMLElement) {
  const from = window.scrollY;
  const margin = parseFloat(getComputedStyle(target).scrollMarginTop) || 0;
  const to = Math.max(0, Math.min(from + target.getBoundingClientRect().top - margin,
    document.documentElement.scrollHeight - window.innerHeight));
  const started = performance.now();
  let frame = 0;
  const cancel = () => {
    cancelAnimationFrame(frame);
    window.removeEventListener('wheel', cancel);
    window.removeEventListener('touchstart', cancel);
    window.removeEventListener('keydown', cancel);
  };
  const step = (now: number) => {
    const progress = Math.min(1, (now - started) / 420);
    window.scrollTo({ top: from + (to - from) * (1 - (1 - progress) ** 3), behavior: 'instant' });
    if (progress < 1) frame = requestAnimationFrame(step);
    else cancel();
  };
  window.addEventListener('wheel', cancel, { passive: true });
  window.addEventListener('touchstart', cancel, { passive: true });
  window.addEventListener('keydown', cancel);
  frame = requestAnimationFrame(step);
  return cancel;
}

// Keep this inside Suspense so navigation finishes after the destination DOM
// exists, including the first lazy-loaded visit to documentation.
export function RouteEffects({ navigation, children }: { navigation: Navigation; children: ReactNode }) {
  useLayoutEffect(() => {
    let target: HTMLElement | null = null;
    try { target = document.getElementById(decodeURIComponent(navigation.hash.slice(1))); } catch { /* Ignore malformed fragments. */ }
    if (navigation.action !== 'initial') {
      const focusTarget = target ?? document.getElementById('main');
      if (focusTarget && !focusTarget.hasAttribute('tabindex')) focusTarget.setAttribute('tabindex', '-1');
      focusTarget?.focus({ preventScroll: true });
    }
    if (navigation.action === 'pop' && navigation.position) {
      window.scrollTo({ left: navigation.position[0], top: navigation.position[1], behavior: 'instant' });
    } else if (target) {
      if (navigation.action === 'push' && navigation.pathname === '/' && target.id === 'install' &&
        !window.matchMedia('(prefers-reduced-motion: reduce)').matches) return scrollToInstallation(target);
      target.scrollIntoView({ behavior: 'instant' });
    } else if (navigation.action !== 'initial') {
      window.scrollTo({ left: 0, top: 0, behavior: 'instant' });
    }
  }, [navigation]);
  return children;
}
