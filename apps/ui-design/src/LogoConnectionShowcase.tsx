import { useEffect, useState } from 'react';
import { Button, ConnectionLogo, Switch, useChoiceScroll } from '@molr/ui';
import type { ConnectionState } from '@molr/ui';
import { Play, RotateCcw, Square } from 'lucide-react';
import { productMessages } from './i18n';
import type { Locale } from './messages';
import './logo-connection-showcase.css';

const states: ConnectionState[] = ['connected', 'reconnecting', 'disconnected', 'failed', 'recovered'];
const animations = { connected: 'daily', reconnecting: 'loading', disconnected: 'static', failed: 'static', recovered: 'greet' } as const;

export function LogoConnectionShowcase({ locale, markOnly }: { locale: Locale; markOnly: boolean }) {
  const t = productMessages[locale].logoConnectionShowcase;
  const [state, setState] = useState<ConnectionState>('connected');
  const [scenario, setScenario] = useState<'success' | 'failure' | null>(null), [replay, setReplay] = useState(0), [reduced, setReduced] = useState(false);
  const choices = useChoiceScroll(state);
  useEffect(() => {
    if (state === 'recovered') {
      const timer = setTimeout(() => { setState('connected'); setScenario(null); }, 2400);
      return () => clearTimeout(timer);
    }
    if (!scenario || (state !== 'disconnected' && state !== 'reconnecting')) return;
    const timer = setTimeout(() => {
      const next = state === 'disconnected' ? 'reconnecting' : scenario === 'failure' ? 'failed' : 'recovered';
      setState(next);
      if (next === 'failed') setScenario(null);
    }, state === 'disconnected' ? 1600 : 3200);
    return () => clearTimeout(timer);
  }, [state, scenario, replay]);
  function select(next: ConnectionState) { setScenario(null); setState(next); setReplay(value => value + 1); }
  function play(outcome: 'success' | 'failure', retry = false) { setState(retry ? 'reconnecting' : 'disconnected'); setReplay(value => value + 1); setScenario(outcome); }
  const animation = reduced && state !== 'reconnecting' ? 'static' : animations[state];
  return <section className="logo-connection" id="logo-connection" aria-labelledby="logo-connection-title" data-reduced-motion={reduced || undefined}>
    <div className="logo-connection__heading"><h3 id="logo-connection-title">{t.title}</h3><p>{t.description}</p></div>
    <div className="logo-connection__controls">
      <div ref={choices} className="ui-choice-list ui-choice-list--segmented" role="group" aria-label={t.controls}>
        {states.map(value => <button key={value} type="button" aria-pressed={state === value} aria-controls="logo-connection-previews" onClick={() => select(value)}>{t.states[value]}</button>)}
      </div>
      <div className="logo-control-actions">
        <Button variant="secondary" size="sm" onClick={() => {
          if (scenario) { setScenario(null); setState('connected'); }
          else play('success');
        }}>{scenario ? <Square /> : <Play />}{scenario ? t.stop : t.play}</Button>
        <Button variant="secondary" size="sm" disabled={!!scenario} onClick={() => play('failure')}><Play />{t.playFailure}</Button>
        <Button variant="ghost" size="sm" disabled={animation === 'static' && (state !== 'failed' || reduced)} onClick={() => { setScenario(null); setReplay(value => value + 1); }}><RotateCcw />{t.replay}</Button>
        <Switch label={t.reduceMotion} checked={reduced} onChange={event => setReduced(event.target.checked)} />
      </div>
    </div>
    <div className="logo-connection__status" role="status" data-connection={state}><strong>{t.states[state]}</strong><span>{t.details[state]}</span>{state === 'failed' && <Button variant="secondary" size="sm" onClick={() => play('success', true)}><RotateCcw />{t.retry}</Button>}</div>
    <div className="logo-examples" id="logo-connection-previews" data-connection={state}>
      {(['dark', 'light'] as const).map(variant => <div key={variant} className={`logo-connection__panel logo-connection__panel--${variant === 'dark' ? 'light' : 'dark'}`}>
        <span className="logo-connection__caption">{variant === 'dark' ? t.light : t.dark}</span>
        <div className="logo-connection__samples">
          <div><ConnectionLogo state={state} labels={t.states} variant={variant} replayKey={replay} markOnly reducedMotion={reduced} interactive={false} className="logo-connection__target logo-connection__target--navigation" /><small>{t.navigation}</small></div>
          <div><ConnectionLogo state={state} labels={t.states} variant={variant} replayKey={replay} markOnly={markOnly} reducedMotion={reduced} interactive={false} className="logo-connection__target logo-connection__target--large" /><small>{markOnly ? t.mark : t.wordmark}</small></div>
        </div>
      </div>)}
    </div>
    <p className="logo-connection__note">{t.hint}</p><p className="logo-connection__note">{t.preview}</p>
  </section>;
}
