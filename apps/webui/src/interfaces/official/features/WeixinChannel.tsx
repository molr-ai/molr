import { useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Badge, Button, Dialog, TextField } from '@molr/ui';
import QRCode from 'qrcode';
import { poll } from '../../../polling';
import { api } from '../../../api/client';
import type { Assistant, WeixinLogin, WeixinStatus } from '../../../api/types';
import { useCopy, useData, useQuery } from '../../../state';
import { weixinCopy } from '../../../weixin-copy';
import { Confirm, ErrorNotice, useAction } from '../components/common';
import './weixin.css';

export function ChatChannelDialog({ assistant, onClose }: { assistant: Assistant; onClose: () => void }) {
 const { locale } = useCopy(), t = weixinCopy[locale], { invalidateTopics } = useData();
 const status = useQuery<WeixinStatus>(`/api/assistants/${assistant.ID}/weixin`);
 const action = useAction();
 const [recognized, setRecognized] = useState(false);
 const [login, setLogin] = useState<WeixinLogin | null>(null), [disconnect, setDisconnect] = useState(false);
 const bound = status.data?.Channel === 'weixin';
 const changed = () => invalidateTopics(['weixin']);
 async function begin() { await action.run(async () => { setRecognized(false); const next = await api.post<WeixinLogin>('/api/weixin/login', { AssistantID: assistant.ID }); if (next.Status === 'already_connected') { setLogin(null); setRecognized(true); changed(); } else setLogin(next); }, undefined, false); }
 return <Dialog open title={t.title} description={assistant.Name} dismissible={!action.busy && !login && !disconnect} onOpenChange={open => { if (!open) onClose(); }}>
 <section className="weixin-settings" aria-label={t.current}>
  <div className="record-heading"><h3>{t.current}</h3>{status.data && <Badge tone={bound ? 'positive' : 'neutral'}>{bound ? t.weixin : t.webui}</Badge>}</div>
  <p className="muted">{t.help}</p><p className="muted">{t.mediaSupport}</p>
  {recognized && <Alert tone="positive" title={t.states.already_connected} />}
  {!login && !disconnect && <ErrorNotice error={action.error || status.error} retry={action.error ? begin : status.reload} />}
  {bound && status.data && <>{status.data.State === 'connected' ? <Alert tone="positive" title={t.states.connected} /> : <Alert tone="warning" title={t.states[status.data.State]} />}{status.data.State === 'connected' && !status.data.LastInboundAt && <p>{t.firstMessage}</p>}</>}
  <div className="form-actions">
   {(!bound || status.data?.State === 'login_required') && <Button variant="secondary" disabled={assistant.State !== 'active' || !status.data} loading={action.busy} onClick={() => void begin()}>{bound ? t.reconnect : t.connect}</Button>}
   {bound && <Button variant="secondary" onClick={() => setDisconnect(true)}>{t.disconnect}</Button>}
  </div>
  {login && <LoginDialog assistantID={assistant.ID} login={login} onChange={setLogin} onClose={() => { setLogin(null); changed(); }} onRefresh={begin} />}
  {disconnect && <Confirm title={t.disconnectTitle} description={t.disconnectHelp} onClose={() => setDisconnect(false)} onConfirm={async () => { await api.post('/api/weixin/disconnect', { AssistantID: assistant.ID }); changed(); }} />}
 </section>
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
 return <Dialog open title={t.scan} dismissible={!action.busy} onOpenChange={open => { if (!open) void cancel(); }}>
  <div className="weixin-login"><p>{t.confirm}</p>
   <Alert title={t.privacyTitle}>{t.privacy}</Alert>
   {!terminal && login.QRCode && <LocalQRCode value={login.QRCode} label={t.scan} />}
   <p role="status">{expired ? t.expired : t.states[login.Status]}</p>
   {login.Status === 'need_verifycode' && !expired && <form onSubmit={event => { event.preventDefault(); verify.current = code.trim(); onChange({ ...login, Status: 'scaned' }); }}>
    <TextField label={t.verify} value={code} maxLength={32} required autoComplete="one-time-code" onChange={event => setCode(event.target.value)} /><p className="muted">{t.verifyHelp}</p><Button type="submit" disabled={!code.trim()}>{t.submit}</Button>
   </form>}
   <ErrorNotice error={error || action.error} />
   <div className="form-actions"><Button variant="secondary" loading={action.busy} onClick={() => void cancel()}>{t.cancel}</Button>{terminal && <Button onClick={() => void action.run(onRefresh, undefined, false)}>{t.refresh}</Button>}</div>
  </div>
 </Dialog>;
}
function LocalQRCode({ value, label }: { value: string; label: string }) {
 const qr = useMemo(() => { try { return QRCode.create(value, { errorCorrectionLevel: 'M' }); } catch { return null; } }, [value]);
 if (!qr) return <Alert tone="danger" title={label} />;
 const size = qr.modules.size, paths: string[] = [];
 for (let row = 0; row < size; row++) for (let col = 0; col < size; col++) if (qr.modules.get(row, col)) paths.push(`M${col + 4} ${row + 4}h1v1h-1z`);
 return <svg role="img" aria-label={label} className="weixin-qr" viewBox={`0 0 ${size + 8} ${size + 8}`} shapeRendering="crispEdges"><rect width={size + 8} height={size + 8} fill="var(--color-qr-background)" /><path d={paths.join('')} fill="var(--color-qr-foreground)" /></svg>;
}
