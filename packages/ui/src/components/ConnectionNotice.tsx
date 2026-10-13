import { CircleAlert, RefreshCw, WifiOff } from 'lucide-react';
import { useUILocale } from '../locale';
import { Button } from './ui';
import type { ConnectionState } from './ConnectionLogo';

export type ConnectionNoticeProps = { connection: ConnectionState; onRetry: () => void; className?: string };

/** Presentation only: connection events and retry timing belong to the caller. */
export function ConnectionNotice({ connection, onRetry, className = '' }: ConnectionNoticeProps) {
  const { messages, reducedMotion } = useUILocale(), copy = messages.connectionNotice;
  if (connection === 'connected' || connection === 'recovered') return null;
  const reconnecting = connection === 'reconnecting';
  const Icon = reconnecting ? RefreshCw : connection === 'failed' ? CircleAlert : WifiOff;
  return <div className={`connection-banner ${className}`} data-connection={connection} data-reduced-motion={reducedMotion || undefined} role="status" aria-atomic="true">
    <Icon className="connection-banner__icon" size={16} aria-hidden="true" />
    <span className="connection-banner__message">{copy[connection]}</span>
    {!reconnecting && <Button variant="ghost" size="sm" onClick={onRetry}>{copy.retry}</Button>}
  </div>;
}
