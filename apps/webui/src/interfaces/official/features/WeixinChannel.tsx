import { useEffect, useRef, useState } from 'react';
import { Alert, Badge, Button, Drawer, IconButton, ModalPresence, TextField, useBreakpoint } from '@molr/ui';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import type { Assistant, WeixinLogin, WeixinStatus, TelegramStatus, SlackStatus, DingtalkStatus, FeishuStatus, QQStatus } from '@molr/sdk';
import { api } from '../../../api/client';
import { useCopy, useData, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { poll } from '../../../polling';
import { weixinCopy } from '../../../weixin-copy';
import { Confirm, Loading, useAction } from '../components/common';
import { LocalQRCode } from './ChannelQRCode';
import { TelegramChannel } from './TelegramChannel';
import { SlackChannel } from './SlackChannel';
import { DingtalkChannel } from './DingtalkChannel';
import { FeishuChannel } from './FeishuChannel';
import { QQChannel } from './QQChannel';
import { ChannelAppIcon } from './ChannelAppIcon';
import './weixin.css';

type Channel = 'weixin' | 'qq' | 'telegram' | 'slack' | 'dingtalk' | 'feishu';
const channels: Channel[] = ['weixin', 'qq', 'feishu', 'dingtalk', 'telegram', 'slack'];
const names = { zh: { weixin: '微信', qq: 'QQ', telegram: 'Telegram', slack: 'Slack', dingtalk: '钉钉', feishu: '飞书' }, en: { weixin: 'Weixin', qq: 'QQ', telegram: 'Telegram', slack: 'Slack', dingtalk: 'DingTalk', feishu: 'Feishu' } };
export function ChatChannelDrawer({ assistant, onClose }: { assistant: Assistant; onClose: () => void }) {
 const { locale } = useCopy(), t = weixinCopy[locale], mobile = useBreakpoint('mobile');
 const weixin = useQuery<WeixinStatus>(`/api/assistants/${assistant.ID}/weixin`);
 const qq = useQuery<QQStatus>(`/api/assistants/${assistant.ID}/qq`);
 const telegram = useQuery<TelegramStatus>(`/api/assistants/${assistant.ID}/telegram`);
 const slack = useQuery<SlackStatus>(`/api/assistants/${assistant.ID}/slack`);
 const dingtalk = useQuery<DingtalkStatus>(`/api/assistants/${assistant.ID}/dingtalk`);
 const feishu = useQuery<FeishuStatus>(`/api/assistants/${assistant.ID}/feishu`);
 const queries = { weixin, qq, telegram, slack, dingtalk, feishu };
 const ready = channels.every(key => !!queries[key].data);
 const bound = channels.find(key => queries[key].data?.Channel === key);
 const reserved = channels.find(key => ['qq', 'slack', 'dingtalk'].includes(key) && !!queries[key].data && !['disconnected', 'expired'].includes(queries[key].data!.State));
 const [selected, setSelected] = useState<Channel | null>(null);
 const lastBound = useRef<Channel | undefined>(undefined), lastSelected = useRef<Channel | null>(null);
 const back = useRef<HTMLButtonElement>(null), triggers = useRef<Partial<Record<Channel, HTMLButtonElement>>>({});
 const current = bound ?? selected;
 useEffect(() => {
  if (bound) setSelected(bound);
  else if (lastBound.current) { const previous = lastBound.current; setSelected(null); requestAnimationFrame(() => triggers.current[previous]?.focus()); }
  lastBound.current = bound;
 }, [bound]);
 useEffect(() => { if (current && !bound) back.current?.focus(); }, [current, bound]);
 const failed = channels.find(key => queries[key].error);
 useErrorToast(failed ? queries[failed].error : null, failed ? queries[failed].reload : undefined);
 const returnToList = () => { if (selected) { const previous = selected; setSelected(null); requestAnimationFrame(() => triggers.current[previous]?.focus()); } };
 if (current) lastSelected.current = current;
 const shown = current ?? lastSelected.current;
 const disabled = !ready || !!reserved && reserved !== shown;
 return <Drawer open title={t.title} description={assistant.Name} className="chat-channel-drawer" side={mobile ? 'bottom' : 'right'} onOpenChange={open => { if (!open) onClose(); }}
  headerContent={current ? <div className="channel-heading">{!bound && <IconButton ref={back} label={locale === 'zh' ? '返回入口列表' : 'Back to channels'} onClick={returnToList}><ChevronLeft size={18} /></IconButton>}<span>{names[locale][current]}</span>{bound && <Badge tone="positive">{locale === 'zh' ? '已绑定' : 'Bound'}</Badge>}</div> : undefined}>
  {!ready ? <Loading /> : <div className="channel-viewport"><div className="channel-pages" data-detail={!!current}>
   <div className="channel-page channel-list" inert={!!current} aria-hidden={!!current}>
    <p className="muted">{locale === 'zh' ? '选择一个聊天入口，与这位助理继续聊天。已有记录和上下文会保留。' : 'Choose where to chat with this assistant. Your history and context stay with you.'}</p>
    {channels.map(key => <button type="button" key={key} ref={node => { triggers.current[key] = node!; }} className="channel-choice" onClick={() => setSelected(key)}><ChannelAppIcon channel={key} /><span>{names[locale][key]}</span>{reserved === key && <Badge>{locale === 'zh' ? '待配对' : 'Pairing'}</Badge>}<ChevronRight size={18} aria-hidden="true" /></button>)}
   </div>
   <div className="channel-page channel-detail" inert={!current} aria-hidden={!current}>
    {current && <div key={shown}>
     {disabled && <Alert title={locale === 'zh' ? '请先取消其他入口正在进行的配对。' : 'Cancel the pending pairing in the other channel first.'} />}
     {shown === 'weixin' && <WeixinChannel assistant={assistant} status={weixin.data!} disabled={disabled} />}
     {shown === 'qq' && <QQChannel assistant={assistant} status={qq.data!} disabled={disabled} />}
     {shown === 'telegram' && <TelegramChannel assistant={assistant} onClose={returnToList} disabled={disabled} />}
     {shown === 'slack' && <SlackChannel assistant={assistant} disabled={disabled} />}
     {shown === 'dingtalk' && <DingtalkChannel assistant={assistant} status={dingtalk.data} reload={dingtalk.reload} disabled={disabled} />}
     {shown === 'feishu' && <FeishuChannel assistant={assistant} status={feishu.data ?? undefined} disabled={disabled} />}
    </div>}
   </div>
  </div></div>}
 </Drawer>;
}
function WeixinChannel({ assistant, status, disabled }: { assistant: Assistant; status: WeixinStatus; disabled: boolean }) {
 const { locale } = useCopy(), t = weixinCopy[locale], { invalidateTopics } = useData(), action = useAction();
 const [login, setLogin] = useState<WeixinLogin | null>(null), [disconnect, setDisconnect] = useState(false);
 const [recognized, setRecognized] = useState(false);
 const activeLogin = useRef<WeixinLogin | null>(null); activeLogin.current = login;
 const changed = () => invalidateTopics(['weixin']);
 const bound = status.Channel === 'weixin';
 useEffect(() => () => { if (activeLogin.current) void api.post('/api/weixin/cancel', { AssistantID: assistant.ID, ID: activeLogin.current.ID }).catch(() => {}); }, [assistant.ID]);
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
