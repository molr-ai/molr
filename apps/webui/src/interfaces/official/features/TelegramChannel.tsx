import { useEffect, useRef, useState } from 'react';
import { Alert, Button, ModalPresence, TextField } from '@molr/ui';
import type { Assistant, TelegramLogin, TelegramStatus } from '@molr/sdk';
import { api } from '@molr/sdk';
import { useCopy, useData, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { poll } from '../../../polling';
import { telegramCopy } from '../../../telegram-copy';
import { Confirm, useAction } from '../components/common';
import { LocalQRCode } from './ChannelQRCode';

export function TelegramChannel({ assistant, onClose, disabled = false }: { assistant: Assistant; onClose: () => void; disabled?: boolean }) {
 const { locale } = useCopy(), t = telegramCopy[locale], { invalidateTopics } = useData();
 const status = useQuery<TelegramStatus>(`/api/assistants/${assistant.ID}/telegram`), action = useAction();
 const [token, setToken] = useState(''), [login, setLogin] = useState<TelegramLogin | null>(null), [expired, setExpired] = useState(false), [disconnect, setDisconnect] = useState(false), [pollError, setPollError] = useState<unknown>(null);
 const activeLogin = useRef<TelegramLogin | null>(null); activeLogin.current = login;
 useEffect(() => () => { if (activeLogin.current) void api.post('/api/telegram/cancel', { AssistantID: assistant.ID, ID: activeLogin.current.ID }).catch(() => {}); }, [assistant.ID]);
 const bound = status.data?.Channel === 'telegram';
 useErrorToast(action.error || pollError || status.error);
 useEffect(() => { if (!login) return; const timer = setTimeout(() => setExpired(true), Math.max(0, Date.parse(login.ExpiresAt) - Date.now())); return () => clearTimeout(timer); }, [login?.ID, login?.ExpiresAt]);
 useEffect(() => {
  if (!login || login.Status !== 'waiting' || expired) return;
  const loop = poll(async signal => {
   try {
    const next = await api.read<TelegramLogin>('/api/telegram/poll', { AssistantID: assistant.ID, ID: login.ID }, signal);
    if (signal.aborted) return false;
    setLogin(next); setPollError(null); return next.Status === 'ready' ? false : 1500;
   } catch (error) { if (!signal.aborted) setPollError(error); throw error; }
  });
  return () => loop.stop();
 }, [assistant.ID, login?.ID, login?.Status, expired]);
 const changed = () => invalidateTopics(['telegram']);
 async function close() { await action.run(async () => { if (login) await api.post('/api/telegram/cancel', { AssistantID: assistant.ID, ID: login.ID }); setToken(''); setLogin(null); activeLogin.current = null; onClose(); }, undefined, false); }
 async function begin() { await action.run(async () => { const next = await api.post<TelegramLogin>('/api/telegram/login', { AssistantID: assistant.ID, Token: token }); setToken(''); setExpired(false); setLogin(next); }, undefined, false); }
 async function confirm() { await action.run(async () => { await api.post('/api/telegram/confirm', { AssistantID: assistant.ID, ID: login!.ID }); setLogin(null); activeLogin.current = null; changed(); }, undefined, false); }
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
