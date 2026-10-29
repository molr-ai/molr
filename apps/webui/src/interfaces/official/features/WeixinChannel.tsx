import { useEffect, useRef, useState } from 'react';
import { Alert, Button, ModalPresence, TextField } from '@molr/ui';
import type { Assistant, WeixinLogin, WeixinStatus } from '@molr/sdk';
import { api } from '@molr/sdk';
import { useCopy, useData } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useChannelPairing, usePairingPoll } from './use-channel-pairing';
import { weixinCopy } from '../../../weixin-copy';
import { Confirm, useAction } from '../components/common';
import { LocalQRCode } from './ChannelQRCode';
import './weixin.css';

export function WeixinChannel({ assistant, status, disabled }: { assistant: Assistant; status: WeixinStatus; disabled: boolean }) {
 const { locale } = useCopy(), t = weixinCopy[locale], { invalidateTopics } = useData(), action = useAction();
 const { login, setLogin } = useChannelPairing<WeixinLogin>(assistant.ID, 'weixin');
 const [disconnect, setDisconnect] = useState(false);
 const [recognized, setRecognized] = useState(false);
 const changed = () => invalidateTopics(['weixin']);
 const bound = status.Channel === 'weixin';
 async function begin() { await action.run(async () => { const next = await api.post<WeixinLogin>('/api/weixin/login', { AssistantID: assistant.ID }); if (next.Status === 'already_connected') { setLogin(null); setRecognized(true); changed(); } else setLogin(next); }, undefined, false); }
 useErrorToast(action.error, begin);
 return <section className="weixin-settings" aria-label={t.weixin}>
  <p>{t.help}</p><p className="muted">{t.mediaSupport}</p>
  {recognized && <Alert tone="positive" title={t.states.already_connected} />}
  {bound && <Alert tone={status.State === 'connected' ? 'positive' : 'warning'} title={t.states[status.State]}>{!status.LastInboundAt && t.firstMessage}</Alert>}
  {login ? <WeixinLogin assistantID={assistant.ID} login={login} onChange={setLogin} onClose={() => { setLogin(null); changed(); }} onRefresh={begin} /> : <div className="form-actions">
   {(!bound || status.State === 'login_required') && <Button disabled={disabled || assistant.State !== 'active'} loading={action.busy} onClick={() => void begin()}>{bound ? t.reconnect : t.connect}</Button>}
   {bound && <Button variant="secondary" onClick={() => setDisconnect(true)}>{t.disconnect}</Button>}
  </div>}
  <ModalPresence>{disconnect && <Confirm notify={false} title={t.disconnectTitle} description={t.disconnectHelp} onClose={() => setDisconnect(false)} onConfirm={async () => { await api.post('/api/weixin/disconnect', { AssistantID: assistant.ID }); changed(); }} />}</ModalPresence>
 </section>;
}
function WeixinLogin({ assistantID, login, onChange, onClose, onRefresh }: { assistantID: string; login: WeixinLogin; onChange: (v: WeixinLogin) => void; onClose: () => void; onRefresh: () => Promise<void> }) {
 const { locale } = useCopy(), t = weixinCopy[locale], action = useAction();
 const [code, setCode] = useState('');
 const verify = useRef('');
 const stopped = ['confirmed', 'already_connected', 'expired', 'verify_code_blocked', 'conflict'].includes(login.Status);
 useEffect(() => { setCode(''); verify.current = ''; }, [login.ID, login.ExpiresAt]);
 const { expired, error } = usePairingPoll({
  login, enabled: !stopped && (login.Status !== 'need_verifycode' || !!verify.current),
  read: signal => api.read<WeixinLogin>('/api/weixin/poll', { AssistantID: assistantID, ID: login.ID, VerifyCode: verify.current }, signal),
  receive: next => {
   verify.current = ''; onChange(next);
   if (next.Status === 'confirmed' || next.Status === 'already_connected') { onClose(); return false; }
   return ['expired', 'verify_code_blocked', 'conflict', 'need_verifycode'].includes(next.Status) ? false : 1200;
  },
 });
 const terminal = stopped || expired;
 async function cancel() { await action.run(async () => { await api.post('/api/weixin/cancel', { AssistantID: assistantID, ID: login.ID }); onClose(); }, undefined, false); }
 useErrorToast(error || action.error);
  return <section aria-label={t.scan}>
  <div className="weixin-login"><p>{t.confirm}</p>
   <Alert title={t.privacyTitle}>{t.privacy}</Alert>
   {!terminal && login.QRCode && <LocalQRCode value={login.QRCode} label={t.scan} />}
   <p role="status">{expired ? t.expired : t.states[login.Status]}</p>
   {login.Status === 'need_verifycode' && !expired && <form onSubmit={event => { event.preventDefault(); verify.current = code.trim(); onChange({ ...login, Status: 'scaned' }); }}>
    <TextField label={t.verify} value={code} maxLength={32} required autoComplete="one-time-code" onChange={event => setCode(event.target.value)} /><p className="muted">{t.verifyHelp}</p><Button type="submit" disabled={!code.trim()}>{t.submit}</Button>
   </form>}
   <div className="form-actions"><Button variant="secondary" loading={action.busy} onClick={() => void cancel()}>{t.cancel}</Button>{terminal && <Button onClick={() => void action.run(onRefresh, undefined, false)}>{t.refresh}</Button>}</div>
  </div>
 </section>;
}
