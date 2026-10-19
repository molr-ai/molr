import { useEffect, useRef, useState } from 'react';
import { Alert, Button, ModalPresence, TextField } from '@molr/ui';
import type { Assistant, QQLogin, QQStatus } from '@molr/sdk';
import { api } from '../../../api/client';
import { useCopy, useData } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { poll } from '../../../polling';
import { qqCopy } from '../../../qq-copy';
import { Confirm, useAction } from '../components/common';
import { LocalQRCode } from './ChannelQRCode';

export function QQChannel({ assistant, status, disabled }: { assistant: Assistant; status: QQStatus; disabled: boolean }) {
 const { locale } = useCopy(), t = qqCopy[locale], { invalidateTopics } = useData(), action = useAction();
 const [login, setLogin] = useState<QQLogin | null>(null), [manual, setManual] = useState(false), [disconnect, setDisconnect] = useState(false);
 const [appID, setAppID] = useState(''), [secret, setSecret] = useState(''), [expired, setExpired] = useState(false), [error, setError] = useState<unknown>(null);
 const bound = status.Channel === 'qq';
 const pairing = !bound && (!!login?.Code || !['disconnected', 'expired'].includes(status.State));
 const changed = () => invalidateTopics(['qq']);
 const active = useRef(login); active.current = login;
 useEffect(() => () => { if (active.current?.ID && active.current.Status === 'waiting') void api.post('/api/qq/cancel', { AssistantID: assistant.ID, ID: active.current.ID }).catch(() => {}); }, [assistant.ID]);
 useEffect(() => { if (bound) { setLogin(null); setManual(false); setSecret(''); } }, [bound]);
 useEffect(() => { if (!login) return; setExpired(false); const timer = setTimeout(() => setExpired(true), Math.max(0, Date.parse(login.ExpiresAt) - Date.now())); return () => clearTimeout(timer); }, [login?.ID, login?.ExpiresAt]);
 useEffect(() => {
  if (!login || login.Status !== 'waiting' || expired) return;
  const loop = poll(async signal => {
   try { const next = await api.read<QQLogin>('/api/qq/poll', { AssistantID: assistant.ID, ID: login.ID }, signal); if (signal.aborted) return false; setError(null); setLogin(next); if (next.Status !== 'waiting') { changed(); return false; } return 2000; }
   catch (err) { if (!signal.aborted) setError(err); throw err; }
  }); return () => loop.stop();
 }, [assistant.ID, login?.ID, login?.Status, expired]);
 useEffect(() => { if (!pairing) return; const timer = setInterval(changed, 2500); return () => clearInterval(timer); }, [pairing]);
 useErrorToast(action.error || error);
 const begin = () => action.run(async () => { setError(null); setLogin(await api.post<QQLogin>('/api/qq/login', { AssistantID: assistant.ID })); }, undefined, false);
 const cancel = () => action.run(async () => { if (login?.ID) await api.post('/api/qq/cancel', { AssistantID: assistant.ID, ID: login.ID }); if (pairing) await api.post('/api/qq/disconnect', { AssistantID: assistant.ID }); setLogin(null); changed(); }, undefined, false);
 return <section className="weixin-settings" aria-label={t.name}>
  <p>{t.help}</p><p className="muted">{t.media}</p>
  {bound ? <><Alert tone={status.State === 'connected' ? 'positive' : 'warning'} title={t.states[status.State]} /><p>App ID: {status.AppID}</p><Button variant="secondary" onClick={() => setDisconnect(true)}>{t.disconnect}</Button></> : pairing ? <>
   <Alert title={expired ? t.states.expired : t.states.pairing} /><p>{t.pair}</p>{login?.Code ? <TextField label={t.code} value={login.Code} readOnly /> : <p>{t.lost}</p>}
   <Button variant="secondary" loading={action.busy} onClick={() => void cancel()}>{t.cancel}</Button>
  </> : login ? <div className="weixin-login">
   <p>{t.privacy}</p>{!expired && login.Status === 'waiting' && login.QRCode && <><LocalQRCode value={login.QRCode} label={t.scan} /><a href={login.QRCode} target="_blank" rel="noreferrer">{t.open}</a></>}
   <p role="status">{expired ? t.states.expired : t.states[login.Status]}</p>
   <div className="form-actions"><Button variant="secondary" loading={action.busy} onClick={() => void cancel()}>{t.cancel}</Button>{(expired || login.Status !== 'waiting') && <Button loading={action.busy} onClick={() => void begin()}>{t.refresh}</Button>}</div>
  </div> : manual ? <form onSubmit={event => { event.preventDefault(); void action.run(async () => { setLogin(await api.post<QQLogin>('/api/qq/connect', { AssistantID: assistant.ID, AppID: appID.trim(), AppSecret: secret })); setSecret(''); setManual(false); changed(); }, undefined, false); }}>
   <p>{t.manualHelp} <a href="https://q.qq.com/qqbot/openclaw/login.html" target="_blank" rel="noreferrer">QQ</a></p><p className="muted">{t.privacy}</p>
   <TextField label={t.appID} value={appID} maxLength={32} required autoComplete="off" onChange={e => setAppID(e.target.value)} />
   <TextField label={t.secret} type="password" value={secret} maxLength={8192} required autoComplete="new-password" onChange={e => setSecret(e.target.value)} />
   <div className="form-actions"><Button type="submit" disabled={disabled || !appID.trim() || !secret.trim()} loading={action.busy}>{t.save}</Button><Button variant="secondary" disabled={action.busy} onClick={() => { setManual(false); setSecret(''); }}>{t.cancel}</Button></div>
  </form> : <><p className="muted">{t.privacy}</p><div className="form-actions"><Button disabled={disabled || assistant.State !== 'active'} loading={action.busy} onClick={() => void begin()}>{t.connect}</Button><Button variant="secondary" disabled={disabled || action.busy || assistant.State !== 'active'} onClick={() => setManual(true)}>{t.manual}</Button></div></>}
  <ModalPresence>{disconnect && <Confirm notify={false} title={t.disconnectTitle} description={t.disconnectHelp} onClose={() => setDisconnect(false)} onConfirm={async () => { await api.post('/api/qq/disconnect', { AssistantID: assistant.ID }); changed(); }} />}</ModalPresence>
 </section>;
}
