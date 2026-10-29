import { useEffect, useState } from 'react';
import { Alert, Button, ModalPresence, TextField } from '@molr/ui';
import type { Assistant, SlackLogin, SlackStatus } from '@molr/sdk';
import { api } from '@molr/sdk';
import { useCopy, useData, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { slackCopy } from '../../../slack-copy';
import { Confirm, useAction } from '../components/common';

export function SlackChannel({ assistant, disabled, onStatusChange }: { assistant: Assistant; disabled: boolean; onStatusChange?: (value: { Channel: string; State: string }) => void }) {
 const { locale } = useCopy(), t = slackCopy[locale], { invalidateTopics } = useData();
 const status = useQuery<SlackStatus>(`/api/assistants/${assistant.ID}/slack`), action = useAction();
 const [appToken, setAppToken] = useState(''), [botToken, setBotToken] = useState(''), [userID, setUserID] = useState('');
 const [login, setLogin] = useState<SlackLogin | null>(null), [disconnect, setDisconnect] = useState(false);
 const pending = !!status.data && status.data.State !== 'disconnected' && status.data.Channel !== 'slack';
 useEffect(() => { if (status.data) onStatusChange?.(status.data); }, [status.data?.Channel, status.data?.State]);
 const bound = status.data?.Channel === 'slack';
 const changed = () => invalidateTopics(['slack']);
 useErrorToast(action.error || status.error, action.error ? undefined : status.reload);
 async function connect() { await action.run(async () => { const next = await api.post<SlackLogin>('/api/slack/connect', { AssistantID: assistant.ID, AppToken: appToken.trim(), BotToken: botToken.trim(), UserID: userID.trim() }); setAppToken(''); setBotToken(''); setLogin(next); changed(); }, undefined, false); }
 async function remove() { await api.post('/api/slack/disconnect', { AssistantID: assistant.ID }); setLogin(null); changed(); }
 return <section className="weixin-settings" aria-label={t.name}>
  <h3>{t.name}</h3><p className="muted">{t.help}</p>
  {(bound || pending) && <Alert tone={bound && status.data?.State === 'connected' ? 'positive' : 'warning'} title={status.data?.State === 'reconnecting' ? t.reconnecting : status.data?.State === 'login_required' ? t.login_required : status.data?.State === 'paused' ? t.paused : bound ? t.paired : t.pending} />}
  {pending && login && <><p>{t.pair}</p><code className="slack-pair-code">{login.Code}</code></>}
  {!bound && !pending && <form className="weixin-settings" onSubmit={event => { event.preventDefault(); void connect(); }}>
   <p className="muted">{t.setup}</p><p className="muted">{t.privacy}</p>
   <TextField label={t.appToken} type="password" value={appToken} onChange={e => setAppToken(e.target.value)} autoComplete="off" maxLength={8192} required disabled={disabled || action.busy} />
   <TextField label={t.botToken} type="password" value={botToken} onChange={e => setBotToken(e.target.value)} autoComplete="off" maxLength={8192} required disabled={disabled || action.busy} />
   <TextField label={t.userID} value={userID} onChange={e => setUserID(e.target.value)} autoComplete="off" maxLength={64} required disabled={disabled || action.busy} />
   <div className="form-actions"><Button type="submit" variant="secondary" loading={action.busy} disabled={disabled || assistant.State !== 'active' || !status.data || !appToken.trim() || !botToken.trim() || !userID.trim()}>{t.connect}</Button></div>
  </form>}
  {pending && <div className="form-actions"><Button variant="secondary" loading={action.busy} onClick={() => void action.run(remove, undefined, false)}>{t.cancel}</Button></div>}
  {bound && <div className="form-actions"><Button variant="secondary" onClick={() => setDisconnect(true)}>{t.disconnect}</Button></div>}
  <ModalPresence>{disconnect && <Confirm title={t.disconnectTitle} description={t.disconnectHelp} onClose={() => setDisconnect(false)} onConfirm={remove} />}</ModalPresence>
 </section>;
}
