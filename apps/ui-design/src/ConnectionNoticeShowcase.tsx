import { useEffect, useState } from 'react';
import { Button, ConnectionNotice, SegmentedControl, Switch, UIProvider, useUILocale } from '@molr/ui';
import type { ConnectionState } from '@molr/ui';
import { ComponentExample } from './ComponentExample';
import { libraryMessages } from './library-messages';

const states: ConnectionState[] = ['reconnecting', 'disconnected', 'failed', 'recovered', 'connected'];

export function ConnectionNoticeShowcase() {
  const { locale, messages } = useUILocale(), copy = libraryMessages[locale].connectionNotices;
  const [connection, setConnection] = useState<ConnectionState>('reconnecting');
  const [outcome, setOutcome] = useState<'success' | 'failure' | null>(null);
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    if (!outcome) return;
    const timer = setTimeout(() => { setConnection(outcome === 'success' ? 'recovered' : 'failed'); setOutcome(null); }, 1800);
    return () => clearTimeout(timer);
  }, [outcome]);
  function play(next: 'success' | 'failure') { setConnection('reconnecting'); setOutcome(next); }
  return <ComponentExample id="connection-notices" title={copy.title} description={copy.description} wide>
    <SegmentedControl label={copy.state} hideLabel value={connection} onValueChange={value => { setOutcome(null); setConnection(value as ConnectionState); }} options={states.map(value => ({ value, label: messages.connection[value] }))} />
    <UIProvider locale={locale} reducedMotion={reduced}>
      <div className="connection-notice-preview" style={{ minHeight: 64, display: 'grid', alignItems: 'center' }}>
        <ConnectionNotice connection={connection} onRetry={() => play('success')} />
        {(connection === 'connected' || connection === 'recovered') && <p className="component-copy" role="status">{copy.hidden}</p>}
      </div>
    </UIProvider>
    <div className="component-row">
      <Button size="sm" variant="secondary" disabled={!!outcome} onClick={() => play('success')}>{copy.success}</Button>
      <Button size="sm" variant="secondary" disabled={!!outcome} onClick={() => play('failure')}>{copy.failure}</Button>
      <Switch label={copy.reducedMotion} checked={reduced} onChange={event => setReduced(event.target.checked)} />
    </div>
    <p className="component-copy">{copy.hint}</p>
  </ComponentExample>;
}
