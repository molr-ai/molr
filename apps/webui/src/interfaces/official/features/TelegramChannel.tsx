import { useEffect, useState } from 'react';
import { Alert, Button, ModalPresence, TextField } from '@molr/ui';
import type { Assistant, TelegramLogin, TelegramStatus } from '@molr/sdk';
import { api } from '@molr/sdk';
import { useCopy, useData, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useChannelPairing, usePairingPoll } from './use-channel-pairing';
import { telegramCopy } from '../../../telegram-copy';
import { Confirm, useAction } from '../components/common';
import { LocalQRCode } from './ChannelQRCode';

export function TelegramChannel({ assistant, onClose, disabled = false, onStatusChange }: { assistant: Assistant; onClose: () => void; disabled?: boolean; onStatusChange?: (value: { Channel: string; State: string }) => void }) {
 const { locale } = useCopy(), t = telegramCopy[locale], { invalidateTopics } = useData();
 const status = useQuery<TelegramStatus>(`/api/assistants/${assistant.ID}/telegram`), action = useAction();
 const [token, setToken] = useState(''), [disconnect, setDisconnect] = useState(false);
 const { login, setLogin } = useChannelPairing<TelegramLogin>(assistant.ID, 'telegram');
 useEffect(() => { if (status.data) onStatusChange?.(status.data); }, [status.data?.Channel, status.data?.State]);
 const bound = status.data?.Channel === 'telegram';
 const { expired, error: pollError } = usePairingPoll({
  login, enabled: login?.Status === 'waiting',
  read: signal => api.read<TelegramLogin>('/api/telegram/poll', { AssistantID: assistant.ID, ID: login!.ID }, signal),
  receive: next => { setLogin(next); return next.Status === 'ready' ? false : 1500; },
 });
 useErrorToast(action.error || pollError || status.error);
 const changed = () => invalidateTopics(['telegram']);
 async function close() { await action.run(async () => { if (login) await api.post('/api/telegram/cancel', { AssistantID: assistant.ID, ID: login.ID }); setToken(''); setLogin(null); onClose(); }, undefined, false); }
 async function begin() { await action.run(async () => { const next = await api.post<TelegramLogin>('/api/telegram/login', { AssistantID: assistant.ID, Token: token }); setToken(''); setLogin(next); }, undefined, false); }
 async function confirm() { await action.run(async () => { await api.post('/api/telegram/confirm', { AssistantID: assistant.ID, ID: login!.ID }); setLogin(null); changed(); }, undefined, false); }
 return <section aria-label={t.channel}>
  <div className="weixin-settings">
   {bound && status.data ? <><p>@{status.data.BotUsername}</p><Alert tone={status.data.State === 'connected' ? 'positive' : 'warning'} title={t.states[status.data.State]} /><p>{t.help}</p><Button variant="secondary" onClick={() => setDisconnect(true)}>{t.disconnect}</Button></> : login ? <>
    <p>@{login.BotUsername}</p>
    {expired ? <Alert tone="warning" title={t.expired} /> : login.Status === 'ready' ? <><p>{t.identity}: <strong>{login.UserName}</strong> ({login.UserID})</p><Button loading={action.busy} onClick={() => void confirm()}>{t.confirm}</Button></> : <><LocalQRCode value={login.URL} label={t.open} /><a href={login.URL} target="_blank" rel="noopener noreferrer">{t.open}</a><p role="status">{t.waiting}</p></>}
    <Button variant="secondary" loading={action.busy} onClick={() => void close()}>{t.cancel}</Button>
   </> : <form className="weixin-settings" onSubmit={event => { event.preventDefault(); void begin(); }}>
    <p>{t.setup}</p><p className="muted">{t.privacy}</p>
    <TextField type="password" label={t.token} autoComplete="off" value={token} maxLength={256} required onChange={event => setToken(event.target.value)} />
    <Button type="submit" disabled={disabled || !token.trim() || !status.data || assistant.State !== 'active'} loading={action.busy}>{t.next}</Button>
   </form>}
  </div>
  <ModalPresence>{disconnect && <Confirm notify={false} title={t.disconnectTitle} description={t.disconnectHelp} onClose={() => setDisconnect(false)} onConfirm={async () => { await api.post('/api/telegram/disconnect', { AssistantID: assistant.ID }); changed(); onClose(); }} />}</ModalPresence>
 </section>;
}
