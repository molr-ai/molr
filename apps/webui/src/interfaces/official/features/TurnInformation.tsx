import { useEffect, useState } from 'react';
import { ArrowRight, FlaskConical } from 'lucide-react';
import { Badge, Button, ChatMessageMeta } from '@molr/ui';
import { useMessageFeedback } from './use-message-feedback';
import type { Message, TurnMetrics } from '../../../api/types';
import { useCopy, useData } from '../../../state';
import { MoneyText } from '../components/Money';

export function TurnInformation({ message, revision, onDebug, readOnly = false }: { message: Message; revision: number; onDebug?: (turnID?: string) => void; readOnly?: boolean }) {
  const feedback = useMessageFeedback(message, revision);
  const { t } = useCopy(), { turnMetrics: cache } = useData();
  const copy = t.turnMetrics;
  const [open, setOpen] = useState(false), [visible, setVisible] = useState(document.visibilityState === 'visible');
  const key = `${message.ConversationID}:${message.Source?.ExecutionID || message.ID}`;
  const path = `/api/conversations/${encodeURIComponent(message.ConversationID)}/messages/${encodeURIComponent(message.ID)}/metrics`;
  const [, render] = useState(0);
  const snapshot = cache.snapshot(key, path);
  useEffect(() => {
    if (!open) return;
    const changed = () => setVisible(document.visibilityState === 'visible');
    document.addEventListener('visibilitychange', changed);
    return () => document.removeEventListener('visibilitychange', changed);
  }, [open]);
  useEffect(() => {
    if (!open || !visible) return;
    return cache.subscribe(key, path, () => render(value => value + 1));
  }, [cache, key, path, open, visible]);
  return <ChatMessageMeta {...feedback} showFeedback={!readOnly} metricsTitle={copy.title} metricsOpen={open} onMetricsOpenChange={next => { setVisible(document.visibilityState === 'visible'); setOpen(next); }} metricsContent={open && <div className="turn-information" aria-busy={snapshot.loading}>
    {!snapshot.data && !snapshot.error && <div className="turn-information-loading" role="status">{copy.loading}<i /><i /><i /></div>}
    {!!snapshot.error && <p className="message-info-note" role="alert">{snapshot.data ? copy.stale : copy.failure} <Button size="sm" variant="ghost" onClick={() => void cache.refresh(key, path)}>{t.retry}</Button></p>}
    {snapshot.data && <TurnDetails data={snapshot.data} receivedAt={snapshot.receivedAt} ticking={open && visible} />}
    {onDebug && <footer className="turn-information-footer"><Button className="turn-information-workbench" size="sm" variant="secondary" onClick={() => { setOpen(false); onDebug(snapshot.data?.TurnID || message.Source?.ExecutionID); }}><FlaskConical size={16} aria-hidden="true" /><span>{copy.viewWorkbench}</span><ArrowRight size={16} aria-hidden="true" /></Button></footer>}
  </div>} />;
}

function TurnDetails({ data, receivedAt, ticking }: { data: TurnMetrics; receivedAt?: number; ticking: boolean }) {
  const { t, locale } = useCopy(), copy = t.turnMetrics;
  const [delta, setDelta] = useState(0);
  useEffect(() => {
    setDelta(0);
    if (!ticking || data.EndedAt || data.ElapsedMs == null || receivedAt == null) return;
    const tick = () => setDelta(Math.max(0, performance.now() - receivedAt));
    tick(); const timer = setInterval(tick, 250); return () => clearInterval(timer);
  }, [data, receivedAt, ticking]);
  const number = (value?: number | null, digits = 0) => value == null ? '—' : value.toLocaleString(locale === 'zh' ? 'zh-CN' : 'en-US', { maximumFractionDigits: digits });
  const cacheHitRate = (usage?: TurnMetrics['Usage']) => usage?.InputTokens && usage.InputTokens > 0 && usage.CachedTokens != null ? `${number(usage.CachedTokens / usage.InputTokens * 100, 1)}%` : '—';
  const duration = (value?: number | null) => value == null ? '—' : value < 1000 ? `${number(value)} ms` : `${number(value / 1000, 1)} s`;
  const state = (value: string) => (copy as Record<string, string>)[value] ?? copy.unknown;
  if (data.State === 'unavailable') return <p className="message-info-note">{copy.unavailable}</p>;
  return <>
    <div className="turn-information-status"><Badge tone={data.State === 'done' ? 'positive' : 'neutral'}>{state(data.State)}</Badge>{!data.CostComplete && <small>{copy.pending}</small>}</div>
    <dl className="message-info-details">
      <dt>{copy.input}</dt><dd>{number(data.Usage?.InputTokens)}</dd>
      <dt>{copy.cached}</dt><dd>{cacheHitRate(data.Usage)}</dd>
      <dt>{copy.output}</dt><dd>{number(data.Usage?.OutputTokens)}</dd>
      <dt>{data.CostComplete ? copy.cost : copy.known}</dt><dd>{data.Charges.length ? <MoneyText value={data.Charges[0]} originals={data.Charges} /> : '—'}</dd>
      <dt>{copy.latency}</dt><dd title={data.FirstTokenMs == null ? copy.missing : undefined}>{duration(data.FirstTokenMs)}</dd>
      <dt>{copy.speed}</dt><dd title={data.TokensPerSecond == null ? copy.missing : undefined}>{data.TokensPerSecond == null ? '—' : `${number(data.TokensPerSecond, 1)} token/s`}</dd>
      <dt>{copy.elapsed}</dt><dd>{duration(data.ElapsedMs == null ? null : data.ElapsedMs + delta)}</dd>
    </dl>
    <dl className="message-info-details turn-information-secondary">
      <dt>{copy.models}</dt><dd>{data.Models.join(' · ') || '—'}</dd>
      <dt>{copy.calls}</dt><dd>{data.CompletedCalls} / {data.ModelCalls}</dd>
      <dt>{copy.tools}</dt><dd>{data.ToolCalls}</dd>
      {!!data.Retries && <><dt>{copy.retries}</dt><dd>{data.Retries}</dd></>}
    </dl>
    <p className="message-info-note">{copy.updated} {new Date(data.UpdatedAt).toLocaleTimeString(locale === 'zh' ? 'zh-CN' : 'en-US')}{!data.Finalized && <><br />{copy.partial}</>}</p>
  </>;
}
