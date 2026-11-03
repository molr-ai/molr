import { DelegationReviewContext } from './DelegationPermissionSummary';
import { useId, useState } from 'react';
import { Badge, Button, DateTimePicker, Dialog, IconButton, ModalPresence, SegmentedControl, Select, Switch } from '@molr/ui';
import { MessagesSquare, Pencil } from 'lucide-react';
import type { Assistant, DelegationPermissionRequestInput, PermissionRequest } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useMutation } from '../../../use-mutation';
import { useAction } from '../components/common';
import { PermissionExpiry } from './PermissionScope';

export function AssistantPeerCommunication({ assistant, ready }: { assistant: Assistant; ready: boolean }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const peers = useQuery<Assistant[]>('/api/assistants', true, 0);
  const [editing, setEditing] = useState<Assistant | null>(null);
  useErrorToast(peers.error, peers.reload);
  const h = assistant.PeerCommunication, expired = !!h?.ExpiresAt && Date.parse(h.ExpiresAt) <= Date.now();
  const title = zh ? '与其它助理对话' : 'Talk to other assistants';
  return <article className="assistant-grant" aria-label={title}>
    <div className="assistant-grant-heading">
      <span className="assistant-grant-icon"><MessagesSquare size={16} aria-hidden="true" /></span>
      <div className="assistant-grant-identity assistant-grant-title-line"><h4 className="assistant-grant-title">{title}</h4><Badge size="sm" tone={h?.Enabled && !expired ? 'positive' : 'neutral'}>{!h?.Enabled ? (zh ? '未允许' : 'Not allowed') : expired ? (zh ? '已到期' : 'Expired') : (zh ? '已允许' : 'Allowed')}</Badge></div>
      <div className="assistant-grant-actions"><IconButton size="sm" label={t.edit} disabled={!ready || assistant.State !== 'active'} onClick={() => setEditing(assistant)}><Pencil size={15} /></IconButton></div>
    </div>
    {h?.Enabled ? <dl className="assistant-info-facts assistant-grant-facts">
      <div><dt>{zh ? '助理范围' : 'Assistants'}</dt><dd>{!h || h.AllAssistants ? (zh ? '全部助理' : 'All assistants') : (h.AssistantIDs ?? []).map(id => peers.data?.find(a => a.ID === id)?.Name ?? id).join(zh ? '、' : ', ') || t.none}</dd></div>
      <div><dt>{zh ? '有效期' : 'Validity'}</dt><dd><PermissionExpiry value={h?.ExpiresAt} /></dd></div>
    </dl> : <p className="assistant-info-help assistant-grant-note">{zh ? '开启后，助理可与授权范围内的其它助理互发消息。' : 'When enabled, the assistant can exchange messages with other assistants in its authorized scope.'}</p>}
    <ModalPresence>{editing && <PeerCommunicationEditor assistant={editing} peers={peers.data ?? []} onClose={() => setEditing(null)} />}</ModalPresence>
  </article>;
}

export function PeerCommunicationEditor({ assistant, peers, onClose, request }: { assistant: Assistant; peers: Assistant[]; onClose: () => void; request?: PermissionRequest<DelegationPermissionRequestInput> }) {
  const { t, locale } = useCopy(), zh = locale === 'zh', action = useAction(), formID = useId();
  const [baseline] = useState(assistant);
  const proposal = request?.Input.communication;
  const h = proposal ? { Enabled: true, AllAssistants: proposal.all_assistants, AssistantIDs: proposal.assistant_ids, ExpiresAt: request!.Input.expires_at } : baseline.PeerCommunication;
  const [enabled, setEnabled] = useState(h?.Enabled ?? false);
  const [scope, setScope] = useState<'all' | 'selected'>(h?.AllAssistants === false ? 'selected' : 'all');
  const [ids, setIDs] = useState<string[]>(h?.AssistantIDs ?? []);
  const [expires, setExpires] = useState<string | null>(h?.ExpiresAt ?? null);
  const save = useMutation([assistant.ID, baseline.Version, request?.ID, request?.Version, enabled, scope, ids, expires]);
  const choices = peers.filter(a => !a.EmployerID && a.ID !== assistant.ID && a.State === 'active');
  const invalid = enabled && ((scope === 'selected' && (!ids.length || ids.some(id => !choices.some(a => a.ID === id)))) || (!!expires && Date.parse(expires) <= Date.now()));
  useErrorToast(action.error);
  return <Dialog open title={request ? (zh ? '审核授权' : 'Review permission') : (zh ? '助理对话授权' : 'Assistant communication authorization')} description={assistant.Name} dismissible={!action.busy} onOpenChange={open => { if (!open && !action.busy) onClose(); }} footer={<>
    <Button variant="secondary" disabled={action.busy} onClick={onClose}>{t.cancel}</Button>
    <Button type="submit" form={formID} disabled={invalid} loading={action.busy}>{request ? (zh ? '批准授权' : 'Approve permission') : t.save}</Button>
  </>}><form id={formID} className="editor" onSubmit={event => { event.preventDefault(); if (invalid) return; void action.run(async () => {
    if (request) await save('/api/permission-requests/resolve', { RequestID: request.ID, Decision: 'approve', AssistantVersion: baseline.Version, Communication: { all_assistants: scope === 'all', assistant_ids: scope === 'all' ? [] : ids }, ExpiresAt: expires }, request.Version);
    else await save<Assistant>('/api/assistants/peer-communication', { AssistantID: assistant.ID, Authorization: { Enabled: enabled, AllAssistants: scope === 'all', AssistantIDs: scope === 'all' ? [] : ids, ExpiresAt: expires } }, baseline.Version);
  }, onClose); }}>{request && <DelegationReviewContext assistant={baseline} request={request} />}<fieldset className="assistant-info-fields editor" disabled={action.busy}>
    {!request && <Switch label={zh ? '允许与其它助理对话' : 'Allow communication with other assistants'} checked={enabled} onChange={event => setEnabled(event.target.checked)} />}
    <SegmentedControl label={zh ? '助理范围' : 'Assistant scope'} value={scope} onValueChange={setScope} disabled={action.busy} options={[{ value: 'all', label: zh ? '全部助理' : 'All assistants' }, { value: 'selected', label: zh ? '指定助理' : 'Selected assistants' }]} />
    {scope === 'selected' && <Select multiple label={zh ? '可对话的助理' : 'Allowed assistants'} value={ids} onValueChange={setIDs} disabled={action.busy} options={choices.map(a => ({ value: a.ID, label: a.Name }))} />}
    <DateTimePicker className="permission-expiry-picker" size="md" label={t.resources.expires} value={expires} onValueChange={setExpires} min={new Date().toISOString()} disabled={action.busy} />
    {request && scope === 'selected' && ids.map(id => {
      const peer = choices.find(a => a.ID === id), auth = peer?.PeerCommunication;
      if (!peer || (auth?.Enabled && (!auth.ExpiresAt || Date.parse(auth.ExpiresAt) > Date.now()) && (auth.AllAssistants || auth.AssistantIDs?.includes(assistant.ID)))) return null;
      return <p className="muted" key={id}>{zh ? `${peer.Name} 尚未允许与该助理对话。` : `${peer.Name} has not authorized this conversation. `}<a href={`#/assistants/${peer.ID}`}>{zh ? '查看助理' : 'View assistant'}</a></p>;
    })}
    <p className="muted">{zh ? '对话需要双方助理相互授权；雇主与其临时助理的沟通不受此范围限制。' : 'Both assistants must authorize communication with each other. This scope does not restrict communication between an employer and its temporary assistants.'}</p>
  </fieldset></form></Dialog>;
}
