import { useState } from 'react';
import { Alert, Button, ModalPresence, TextField } from '@molr/ui';
import type { Assistant, FeishuStatus, FeishuLogin } from '@molr/sdk';
import { api } from '@molr/sdk';
import { useCopy, useData } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useChannelPairing, usePairingPoll } from './use-channel-pairing';
import { feishuCopy } from '../../../feishu-copy';
import { Confirm, useAction } from '../components/common';
import { LocalQRCode } from './ChannelQRCode';

export function FeishuChannel({ assistant, status, disabled }: { assistant: Assistant; status?: FeishuStatus; disabled: boolean }) {
 const { locale } = useCopy(), t = feishuCopy[locale], { invalidateTopics } = useData(), action = useAction();
 const { login, setLogin } = useChannelPairing<FeishuLogin>(assistant.ID, 'feishu');
 const [manual, setManual] = useState(false), [disconnect, setDisconnect] = useState(false);
 const [appID, setAppID] = useState(''), [secret, setSecret] = useState(''), [openID, setOpenID] = useState('');
 const changed = () => invalidateTopics(['feishu']);
 const bound = status?.Channel === 'feishu';
 const begin = () => action.run(async () => { setLogin(await api.post<FeishuLogin>('/api/feishu/login', { AssistantID: assistant.ID })); }, undefined, false);
 useErrorToast(action.error);
 async function cancel() { if (!login) return; await action.run(async () => { await api.post('/api/feishu/cancel', { AssistantID: assistant.ID, ID: login.ID }); setLogin(null); }, undefined, false); }
 return <section className="weixin-settings" aria-label={t.name}>
  <h3>{t.name}</h3><p className="muted">{t.help}</p>
  {bound && status && <Alert tone={status.State === 'connected' ? 'positive' : 'warning'} title={t.states[status.State]}>{!status.LastInboundAt && t.firstMessage}</Alert>}
  <div className="form-actions">
   {!login && !manual && (!bound || status?.State === 'credentials_required') && <><Button disabled={disabled || !status || assistant.State !== 'active'} loading={action.busy} onClick={() => void begin()}>{t.connect}</Button><Button variant="secondary" disabled={disabled || !status || assistant.State !== 'active'} onClick={() => setManual(true)}>{t.manual}</Button></>}
   {bound && <Button variant="secondary" onClick={() => setDisconnect(true)}>{t.disconnect}</Button>}
  </div>
  {login && <FeishuLoginDialog login={login} assistantID={assistant.ID} onChange={setLogin} onConnected={() => { setLogin(null); changed(); }} onCancel={() => void cancel()} onRefresh={() => void begin()} busy={action.busy} />}
  {manual && <section aria-label={t.manual}><form onSubmit={event => { event.preventDefault(); void action.run(async () => { await api.post('/api/feishu/connect', { AssistantID: assistant.ID, AppID: appID, AppSecret: secret, OpenID: openID }); setSecret(''); setManual(false); changed(); }, undefined, false); }}>
   <p>{t.manualHelp}</p><p className="muted">{t.privacy}</p>
   <TextField label={t.appID} value={appID} onChange={e => setAppID(e.target.value)} required autoComplete="off" maxLength={128} />
   <TextField label={t.secret} type="password" value={secret} onChange={e => setSecret(e.target.value)} required autoComplete="new-password" maxLength={8192} />
   <TextField label={t.openID} value={openID} onChange={e => setOpenID(e.target.value)} required autoComplete="off" maxLength={128} />
   <div className="form-actions"><Button type="submit" loading={action.busy}>{t.save}</Button></div>
  <Button variant="secondary" onClick={() => { setManual(false); setSecret(''); }}>{t.cancel}</Button></form></section>}
  <ModalPresence>{disconnect && <Confirm notify={false} title={t.disconnectTitle} description={t.disconnectHelp} onClose={() => setDisconnect(false)} onConfirm={async () => { await api.post('/api/feishu/disconnect', { AssistantID: assistant.ID }); changed(); }} />}</ModalPresence>
 </section>;
}
function FeishuLoginDialog({ login, assistantID, onChange, onConnected, onCancel, onRefresh, busy }: { login: FeishuLogin; assistantID: string; onChange: (login: FeishuLogin) => void; onConnected: () => void; onCancel: () => void; onRefresh: () => void; busy: boolean }) {
 const { locale } = useCopy(), t = feishuCopy[locale];
 const { expired, error } = usePairingPoll({
  login, enabled: login.Status === 'waiting',
  read: signal => api.read<FeishuLogin>('/api/feishu/poll', { AssistantID: assistantID, ID: login.ID }, signal),
  receive: next => { if (next.Status === 'confirmed') { onConnected(); return false; } onChange(next); return next.Status === 'waiting' ? 5000 : false; },
 });
 const terminal = expired || login.Status !== 'waiting';
 useErrorToast(error);
 return <section aria-label={t.scan}><div className="weixin-login">
  <p>{t.privacy}</p>
  {!terminal && login.QRCode && <><LocalQRCode value={login.QRCode} label={t.scan} /><a href={login.QRCode} target="_blank" rel="noreferrer">{t.open}</a></>}
  <p role="status">{expired ? t.states.expired : t.states[login.Status]}</p>
  <div className="form-actions"><Button variant="secondary" loading={busy} onClick={onCancel}>{t.cancel}</Button>{terminal && <Button loading={busy} onClick={onRefresh}>{t.refresh}</Button>}</div>
 </div></section>;
}
