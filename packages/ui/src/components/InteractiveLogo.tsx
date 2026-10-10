import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import type { ReactNode, SyntheticEvent } from 'react';
import { getLogoPlayback, getLogoTimeline } from '../logo-playback';
import type { LogoSequence } from '../logo-playback';
import { useUILocale } from '../locale';

type InteractiveLogoProps = {
  src: string;
  source?: string;
  sequence: LogoSequence;
  markOnly: boolean;
  variant: 'dark' | 'light';
  className: string;
  fallback: ReactNode;
  interactionLabel?: string;
};

function subscribeReducedMotion(onChange: () => void) {
  const query = window.matchMedia('(prefers-reduced-motion: reduce)');
  query.addEventListener('change', onChange);
  return () => query.removeEventListener('change', onChange);
}

export function InteractiveLogo({ src, source, sequence, markOnly, variant, className, fallback, interactionLabel }: InteractiveLogoProps) {
  const { messages, reducedMotion: preferReducedMotion } = useUILocale();
  const systemReducedMotion = useSyncExternalStore(subscribeReducedMotion, () => window.matchMedia('(prefers-reduced-motion: reduce)').matches, () => true);
  const reducedMotion = preferReducedMotion || systemReducedMotion;
  const objectRef = useRef<HTMLObjectElement>(null);
  const svgRef = useRef<SVGSVGElement | null>(null);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const [documentUrl, setDocumentUrl] = useState<string>();
  const [phase, setPhase] = useState<ReturnType<typeof getLogoPlayback> | null>(null);
  const timeline = useMemo(() => getLogoTimeline(sequence), [sequence]);

  useEffect(() => {
    if (!source || reducedMotion) {
      // Do not render a revoked URL when animations are enabled again.
      setDocumentUrl(undefined);
      return;
    }
    // Unlike a data URL, a local Blob document keeps same-origin access to
    // the SVG clock for skipping pauses. Release it on replay or unmount.
    const url = URL.createObjectURL(new Blob([source], { type: 'image/svg+xml' }));
    setDocumentUrl(`${url}#${src}`);
    return () => URL.revokeObjectURL(url);
  }, [source, src, reducedMotion]);

  function refreshPhase() {
    const svg = svgRef.current;
    if (!svg) return;
    const next = getLogoPlayback(timeline, svg.getCurrentTime());
    if (!next) { setFailed(true); return; }
    setPhase(previous => previous?.index === next.index && previous.waiting === next.waiting ? previous : next);
  }

  function handleLoad(event: SyntheticEvent<HTMLObjectElement>) {
    // The fallback image can finish before the SVG document. Its load event
    // bubbles through React, but does not mean the object itself is ready.
    if (event.target !== event.currentTarget) return;
    try {
      const root = objectRef.current?.getSVGDocument()?.documentElement as unknown as SVGSVGElement | undefined;
      if (!root || root.localName !== 'svg' || typeof root.getCurrentTime !== 'function') {
        setFailed(true);
        return;
      }
      svgRef.current = root;
      setReady(true);
      refreshPhase();
    } catch {
      setFailed(true);
    }
  }

  useEffect(() => {
    if (!ready || failed || reducedMotion) return;
    // Read the actual SVG clock on return, including any background advancement.
    // Hidden pages do not need to poll for changes to the interaction label.
    let timer: ReturnType<typeof setInterval> | undefined;
    const sync = () => {
      clearInterval(timer);
      timer = undefined;
      if (document.visibilityState !== 'visible') return;
      refreshPhase();
      timer = setInterval(refreshPhase, 150);
    };
    document.addEventListener('visibilitychange', sync);
    sync();
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', sync); };
  }, [ready, failed, reducedMotion, timeline]);

  useEffect(() => {
    if (reducedMotion) {
      svgRef.current = null;
      setReady(false);
      setPhase(null);
    }
  }, [reducedMotion]);

  function skipWait() {
    const svg = svgRef.current;
    if (!svg || reducedMotion) return;
    // Recheck at activation time so repeated clicks cannot skip active clips.
    const current = getLogoPlayback(timeline, svg.getCurrentTime());
    if (!current?.waiting) return;
    // Seek within the current loop; a small offset avoids float rounding back
    // into the preceding hold. The last hold returns to the opening clip.
    svg.setCurrentTime(current.nextStart + 0.001);
    svg.unpauseAnimations();
    refreshPhase();
  }

  if (reducedMotion || failed || (source && !documentUrl)) return fallback;

  const waiting = ready && phase?.waiting === true;
  const label = !ready || !phase ? messages.animationPreparing : waiting
    ? messages.animationWaiting(messages.animationMotions[phase.nextMotion as keyof typeof messages.animationMotions])
    : messages.animationPlaying(messages.animationMotions[phase.motion as keyof typeof messages.animationMotions]);

  return (
    <button
      type="button"
      className={`logo logo--${variant} logo--interactive ${className}`}
      data-phase={waiting ? 'waiting' : 'playing'}
      data-ready={ready || undefined}
      aria-label={interactionLabel ?? label}
      aria-disabled={!waiting}
      onClick={skipWait}
    >
      <object
        ref={objectRef}
        className={markOnly ? 'logo__symbol' : 'logo__wordmark'}
        data={source ? documentUrl : src}
        type="image/svg+xml"
        width={markOnly ? 64 : 183}
        height={markOnly ? 64 : 54}
        style={{ aspectRatio: markOnly ? '1' : '183 / 54' }}
        aria-hidden="true"
        tabIndex={-1}
        onLoad={handleLoad}
        onErrorCapture={event => {
          if (event.target === event.currentTarget) setFailed(true);
        }}
      >
        {fallback}
      </object>
    </button>
  );
}
