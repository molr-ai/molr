import { FeishuChannel } from './FeishuChannel';
import { feishuCopy } from '../../../feishu-copy';
import type { FeishuStatus } from '@molr/sdk';
import { DingtalkChannel } from './DingtalkChannel';
import { dingtalkCopy } from '../../../dingtalk-copy';
import type { DingtalkStatus } from '../../../api/types';
import { SlackChannel } from './SlackChannel';
import { useErrorToast } from '../../../error-toasts';
import { useEffect, useRef, useState } from 'react';
import { ModalPresence, Alert, Badge, Button, Dialog, TextField } from '@molr/ui';
import { LocalQRCode } from './ChannelQRCode';
import { poll } from '../../../polling';
import { api } from '../../../api/client';
import { TelegramChannel } from './TelegramChannel';
import { telegramCopy } from '../../../telegram-copy';
import type { Assistant, SlackStatus, TelegramStatus, WeixinLogin, WeixinStatus } from '../../../api/types';
import { useCopy, useData, useQuery } from '../../../state';
import { weixinCopy } from '../../../weixin-copy';
import { Confirm, useAction } from '../components/common';
import './weixin.css';

export function ChatChannelDialog({ assistant, onClose }: { assistant: Assistant; onClose: () => void }) {
 const { locale } = useCopy(), t = weixinCopy[locale], { invalidateTopics } = useData();
 const status = useQuery<WeixinStatus>(`/api/assistants/${assistant.ID}/weixin`);
 const telegram = useQuery<TelegramStatus>(`/api/assistants/${assistant.ID}/telegram`);
 const telegramBound = telegram.data?.Channel === 'telegram';
 const slack = useQuery<SlackStatus>(`/api/assistants/${assistant.ID}/slack`);
 const ding = useQuery<DingtalkStatus>(`/api/assistants/${assistant.ID}/dingtalk`);
 const feishu = useQuery<FeishuStatus>(`/api/assistants/${assistant.ID}/feishu`);
 const feishuBound = feishu.data?.Channel === 'feishu';
 const dingBound = ding.data?.Channel === 'dingtalk';
 const dingReserved = !!ding.data && !['disconnected', 'expired'].includes(ding.data.State);
 const slackReserved = !!slack.data && slack.data.State !== 'disconnected';
 const [telegramOpen, setTelegramOpen] = useState(false);
 const action = useAction();
 const [recognized, setRecognized] = useState(false);
 const [login, setLogin] = useState<WeixinLogin | null>(null), [disconnect, setDisconnect] = useState(false);
 const bound = status.data?.Channel === 'weixin';
 const changed = () => invalidateTopics(['weixin']);
 async function begin() { await action.run(async () => { setRecognized(false); const next = await api.post<WeixinLogin>('/api/weixin/login', { AssistantID: assistant.ID }); if (next.Status === 'already_connected') { setLogin(null); setRecognized(true); changed(); } else setLogin(next); }, undefined, false); }
 useErrorToast(!login && !disconnect ? action.error || status.error || ding.error || feishu.error : null, action.error ? begin : status.reload);
  return <Dialog open title={t.title} description={assistant.Name} dismissible={!action.busy && !login && !disconnect && !telegramOpen} onOpenChange={open => { if (!open) onClose(); }}>
 <div className="weixin-settings">
 <section className="weixin-settings" aria-label={t.current}>
  <div className="record-heading"><h3>{t.current}</h3>{status.data && <Badge tone={feishuBound || bound || telegramBound || dingBound || slack.data?.Channel === 'slack' ? 'positive' : 'neutral'}>{feishuBound ? feishuCopy[locale].name : dingBound ? dingtalkCopy[locale].name : bound ? t.weixin : telegramBound ? telegramCopy[locale].channel : slack.data?.Channel === 'slack' ? 'Slack' : t.webui}</Badge>}</div>
  {!feishuBound && !dingBound && <><h3>{telegramBound ? telegramCopy[locale].channel : t.weixin}</h3><p className="muted">{telegramBound ? telegramCopy[locale].help : t.help}</p>{!telegramBound && <p className="muted">{t.mediaSupport}</p>}</>}
  {recognized && <Alert tone="positive" title={t.states.already_connected} />}
  {bound && status.data && <>{status.data.State === 'connected' ? <Alert tone="positive" title={t.states.connected} /> : <Alert tone="warning" title={t.states[status.data.State]} />}{status.data.State === 'connected' && !status.data.LastInboundAt && <p>{t.firstMessage}</p>}</>}
  <div className="form-actions">
   {(!bound || status.data?.State === 'login_required') && !telegramBound && !dingBound && !feishuBound && <Button variant="secondary" disabled={assistant.State !== 'active' || !status.data || !telegram.data || !slack.data || !ding.data || !feishu.data || feishuBound || dingReserved || slackReserved} loading={action.busy} onClick={() => void begin()}>{bound ? t.reconnect : t.connect}</Button>}
   {bound && <Button variant="secondary" onClick={() => setDisconnect(true)}>{t.disconnect}</Button>}
   {!bound && !dingBound && !feishuBound && <Button variant="secondary" disabled={!telegram.data || !status.data || !slack.data || !ding.data || !feishu.data || feishuBound || dingReserved || slackReserved || assistant.State !== 'active' && !telegramBound} onClick={() => setTelegramOpen(true)}>{telegramBound ? telegramCopy[locale].manage : telegramCopy[locale].connect}</Button>}
  </div>
  <ModalPresence>{telegramOpen && <TelegramChannel assistant={assistant} onClose={() => { setTelegramOpen(false); invalidateTopics(['telegram']); }} />}</ModalPresence>
  <ModalPresence>{login && <LoginDialog assistantID={assistant.ID} login={login} onChange={setLogin} onClose={() => { setLogin(null); changed(); }} onRefresh={begin} />}</ModalPresence>
  <ModalPresence>{disconnect && <Confirm notify={false} title={t.disconnectTitle} description={t.disconnectHelp} onClose={() => setDisconnect(false)} onConfirm={async () => { await api.post('/api/weixin/disconnect', { AssistantID: assistant.ID }); changed(); }} />}</ModalPresence>
 </section>
 <SlackChannel assistant={assistant} disabled={!status.data || !telegram.data || !ding.data || !feishu.data || feishuBound || dingReserved || bound || telegramBound || !!login || telegramOpen} />
 <DingtalkChannel assistant={assistant} status={ding.data} reload={ding.reload} disabled={!feishu.data || feishuBound || !status.data || !telegram.data || !slack.data || bound || telegramBound || slackReserved || !!login || telegramOpen} />
 <FeishuChannel assistant={assistant} status={feishu.data ?? undefined} disabled={!status.data || !telegram.data || !slack.data || !ding.data || bound || telegramBound || slackReserved || dingReserved || !!login || telegramOpen} />
 </div>
 </Dialog>;
}
function LoginDialog({ assistantID, login, onChange, onClose, onRefresh }: { assistantID: string; login: WeixinLogin; onChange: (v: WeixinLogin) => void; onClose: () => void; onRefresh: () => Promise<void> }) {
 const { locale } = useCopy(), t = weixinCopy[locale], action = useAction();
 const [code, setCode] = useState(''), [error, setError] = useState<unknown>(null), [expired, setExpired] = useState(false);
 const verify = useRef('');
 const terminal = ['confirmed', 'already_connected', 'expired', 'verify_code_blocked', 'conflict'].includes(login.Status) || expired;
 useEffect(() => { setExpired(false); setError(null); setCode(''); verify.current = ''; const timer = setTimeout(() => setExpired(true), Math.max(0, Date.parse(login.ExpiresAt) - Date.now())); return () => clearTimeout(timer); }, [login.ID, login.ExpiresAt]);
 useEffect(() => {
  if (terminal || (login.Status === 'need_verifycode' && !verify.current)) return;
  const loop = poll(async signal => {
   try {
    const next = await api.read<WeixinLogin>('/api/weixin/poll', { AssistantID: assistantID, ID: login.ID, VerifyCode: verify.current }, signal);
    if (signal.aborted) return false;
    verify.current = ''; setError(null); onChange(next);
    if (next.Status === 'confirmed' || next.Status === 'already_connected') { onClose(); return false; }
    return ['expired', 'verify_code_blocked', 'conflict', 'need_verifycode'].includes(next.Status) ? false : 1200;
   } catch (error) { if (!signal.aborted) setError(error); throw error; }
  });
  return () => loop.stop();
 }, [assistantID, login.ID, login.Status, terminal]);
 async function cancel() { await action.run(async () => { await api.post('/api/weixin/cancel', { AssistantID: assistantID, ID: login.ID }); onClose(); }, undefined, false); }
 useErrorToast(error || action.error);
  return <Dialog open title={t.scan} dismissible={!action.busy} onOpenChange={open => { if (!open) void cancel(); }}>
  <div className="weixin-login"><p>{t.confirm}</p>
   <Alert title={t.privacyTitle}>{t.privacy}</Alert>
   {!terminal && login.QRCode && <LocalQRCode value={login.QRCode} label={t.scan} />}
   <p role="status">{expired ? t.expired : t.states[login.Status]}</p>
   {login.Status === 'need_verifycode' && !expired && <form onSubmit={event => { event.preventDefault(); verify.current = code.trim(); onChange({ ...login, Status: 'scaned' }); }}>
    <TextField label={t.verify} value={code} maxLength={32} required autoComplete="one-time-code" onChange={event => setCode(event.target.value)} /><p className="muted">{t.verifyHelp}</p><Button type="submit" disabled={!code.trim()}>{t.submit}</Button>
   </form>}
   <div className="form-actions"><Button variant="secondary" loading={action.busy} onClick={() => void cancel()}>{t.cancel}</Button>{terminal && <Button onClick={() => void action.run(onRefresh, undefined, false)}>{t.refresh}</Button>}</div>
  </div>
 </Dialog>;
}
