import { useSyncExternalStore } from 'react';
import { Logo } from './Logo';
import type { LogoProps } from './Logo';
import { Tooltip } from './Popover';
import type { FloatingSide } from './floating';
import { useUILocale } from '../locale';

export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected' | 'failed' | 'recovered';
export type ConnectionLogoProps = Pick<LogoProps, 'variant' | 'markOnly' | 'assetBaseUrl' | 'className' | 'replayKey'> & {
  state: ConnectionState;
  labels?: Record<ConnectionState, string>;
  onRetry?: () => void;
  retryLabel?: string;
  tooltipSide?: FloatingSide;
  reducedMotion?: boolean;
  interactive?: boolean;
};
function subscribeMotion(change: () => void) {
  const query = window.matchMedia('(prefers-reduced-motion: reduce)');
  query.addEventListener('change', change);
  return () => query.removeEventListener('change', change);
}

/** Connection presentation only; the caller supplies live or preview state. */
export function ConnectionLogo({ state, labels, onRetry, retryLabel, tooltipSide = 'top', reducedMotion = false, interactive = true, className = '', replayKey, ...logoProps }: ConnectionLogoProps) {
  const { messages, reducedMotion: applicationMotion } = useUILocale();
  const systemMotion = useSyncExternalStore(subscribeMotion, () => window.matchMedia('(prefers-reduced-motion: reduce)').matches, () => true);
  const reduced = reducedMotion || applicationMotion || systemMotion;
  const animation = state === 'reconnecting' ? 'loading' : reduced ? 'static' : state === 'connected' ? 'daily' : state === 'recovered' ? 'greet' : 'static';
  const animated = animation !== 'static', playful = interactive && animation === 'daily';
  const retry = (state === 'failed' || state === 'disconnected') && onRetry;
  const status = (labels ?? messages.connection)[state];
  const label = `Molr · ${status}${retry ? ` · ${retryLabel ?? messages.retryConnection}` : ''}`;
  const artwork = <div key={replayKey} className="connection-logo__art" aria-hidden={playful ? undefined : true}>
    {/* Keep the same static image mounted and loaded before the network drops. */}
    <div className="connection-logo__still" aria-hidden="true"><Logo {...logoProps} animation="static" /></div>
    {animated && <Logo {...logoProps} className="connection-logo__motion" animation={animation} replayKey={replayKey} interactive={playful} interactionLabel={label} />}
  </div>;
  return <div className={`connection-logo ${className}`} data-connection={state} data-animated={animated || undefined} data-reduced-motion={reduced || undefined}>
    <Tooltip content={retry ? `${status} · ${retryLabel ?? messages.retryConnection}` : status} width="auto" side={tooltipSide}>
      <span className="connection-logo__trigger" tabIndex={playful || retry ? undefined : 0} role={playful || retry ? undefined : 'img'} aria-label={playful || retry ? undefined : label}>
        {artwork}{retry && <button type="button" className="connection-logo__retry" aria-label={label} onClick={retry} />}
      </span>
    </Tooltip>
  </div>;
}
