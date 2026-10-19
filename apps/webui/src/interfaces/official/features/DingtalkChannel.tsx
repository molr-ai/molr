import { useEffect, useState } from 'react';
import { Alert, Button, ModalPresence, TextField } from '@molr/ui';
import type { Assistant, DingtalkPairing, DingtalkStatus } from '@molr/sdk';
import { api } from '../../../api/client';
import { useCopy, useData } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { dingtalkCopy } from '../../../dingtalk-copy';
import { Confirm, useAction } from '../components/common';

export function DingtalkChannel({ assistant, status, reload, disabled }: { assistant: Assistant; status?: DingtalkStatus; reload: () => void; disabled: boolean }) {
 const { locale } = useCopy(), t = dingtalkCopy[locale], { invalidateTopics } = useData();
 const action = useAction();
 const [editing, setEditing] = useState(false), [disconnect, setDisconnect] = useState(false);
 const [clientID, setClientID] = useState(''), [secret, setSecret] = useState(''), [pairing, setPairing] = useState<DingtalkPairing | null>(null);
 const bound = status?.Channel === 'dingtalk', pending = !!status && status.State !== 'disconnected' && !bound;
 const changed = () => { invalidateTopics(['dingtalk', 'weixin']); reload(); };
 useEffect(() => { if (bound) { setPairing(null); setEditing(false); setSecret(''); } }, [bound]);
 useEffect(() => { if (!pending) return; const timer = setInterval(reload, 2500); return () => clearInterval(timer); }, [pending, reload]);
 useErrorToast(action.error);
 async function connect() { await action.run(async () => { const value = await api.post<DingtalkPairing>('/api/dingtalk/connect', { AssistantID: assistant.ID, ClientID: clientID.trim(), ClientSecret: secret }); setSecret(''); setPairing(value); setEditing(false); changed(); }, undefined, false); }
 async function cancel() { await action.run(async () => { await api.post('/api/dingtalk/disconnect', { AssistantID: assistant.ID }); setPairing(null); changed(); }, undefined, false); }
 return <section className="weixin-settings" aria-label={t.name}>
  <h3>{t.name}</h3><p className="muted">{t.help}</p>
  {status && status.State !== 'disconnected' && <Alert tone={bound && status.State === 'connected' ? 'positive' : ['pairing', 'connecting'].includes(status.State) ? 'neutral' : 'warning'} title={t.states[status.State]} />}
  {pending && <><p>{t.pair}</p>{pairing ? <><TextField label={t.code} readOnly value={pairing.Code} /><p className="muted">{t.expiry}</p></> : <p>{t.lost}</p>}</>}
  <div className="form-actions">
   {!bound && !pending && !editing && <Button variant="secondary" disabled={disabled || !status || assistant.State !== 'active'} onClick={() => setEditing(true)}>{t.connect}</Button>}
   {pending && <Button variant="secondary" loading={action.busy} onClick={() => void cancel()}>{t.cancel}</Button>}
   {bound && <Button variant="secondary" onClick={() => setDisconnect(true)}>{t.disconnect}</Button>}
  </div>
  {editing && <section aria-label={t.title}>
   <form className="weixin-login" onSubmit={event => { event.preventDefault(); void connect(); }}>
    <p>{t.setup}</p><Alert title={t.name}>{t.privacy}</Alert>
    <TextField label={t.clientID} value={clientID} maxLength={256} required autoComplete="off" onChange={event => setClientID(event.target.value)} />
    <TextField label={t.secret} type="password" value={secret} maxLength={8192} required autoComplete="new-password" onChange={event => setSecret(event.target.value)} />
    <Button type="submit" loading={action.busy} disabled={!clientID.trim() || !secret.trim()}>{t.connect}</Button>
   </form>
  <Button variant="secondary" onClick={() => { setEditing(false); setSecret(''); }}>{t.cancel}</Button></section>}
  <ModalPresence>{disconnect && <Confirm notify={false} title={t.disconnectTitle} description={t.disconnectHelp} onClose={() => setDisconnect(false)} onConfirm={async () => { await api.post('/api/dingtalk/disconnect', { AssistantID: assistant.ID }); changed(); }} />}</ModalPresence>
 </section>;
}
