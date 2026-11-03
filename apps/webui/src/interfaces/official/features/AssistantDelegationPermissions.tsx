import { DelegationReviewContext } from './DelegationPermissionSummary';
import { AssistantPeerCommunication } from './AssistantPeerCommunication';
import { Pencil, UsersRound } from 'lucide-react';
import { PermissionExpiry } from './PermissionScope';
import { useId, useState } from 'react';
import { Badge, Button, Checkbox, DateTimePicker, Dialog, IconButton, ModalPresence, Switch, TextField } from '@molr/ui';
import type { Assistant, CurrencyCode, DelegationPermissionRequestInput, Grant, Model, PermissionRequest, Resource } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useMutation } from '../../../use-mutation';
import { grantOptionLabel } from '../../../resource-permissions';
import { currencyText } from '../../../currency';
import { MoneyInput } from '../components/Money';
import { micros, useAction } from '../components/common';

export function AssistantDelegationPermissions({ assistant, models }: { assistant: Assistant; models: Model[] }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const current = useQuery<Assistant>(`/api/assistants/${assistant.ID}`, false, 0);
  const [editing, setEditing] = useState<Assistant | null>(null);
  useErrorToast(current.error, current.reload);
  if (assistant.EmployerID) return null;
  const value = current.data ?? assistant, h = value.Hiring;
  const expired = !!h?.ExpiresAt && Date.parse(h.ExpiresAt) <= Date.now();
  return <section className="assistant-settings-section assistant-hiring-permission" aria-label={zh ? '任务委派' : 'Task delegation'}>
    <h3>{zh ? '任务委派' : 'Task delegation'}</h3>
    <div className="assistant-grant-list"><article className="assistant-grant" aria-label={zh ? '雇佣临时助理' : 'Temporary hiring'}>
      <div className="assistant-grant-heading">
        <span className="assistant-grant-icon"><UsersRound size={16} aria-hidden="true" /></span>
        <div className="assistant-grant-identity assistant-grant-title-line"><h4 className="assistant-grant-title">{zh ? '雇佣临时助理' : 'Temporary hiring'}</h4><Badge size="sm" tone={h?.Enabled && !expired ? 'positive' : 'neutral'}>{!h?.Enabled ? (zh ? '未允许' : 'Not allowed') : expired ? (zh ? '已到期' : 'Expired') : (zh ? '已允许' : 'Allowed')}</Badge></div>
        <div className="assistant-grant-actions"><IconButton size="sm" label={t.edit} disabled={value.State !== 'active' || !current.data} onClick={() => setEditing(value)}><Pencil size={15} /></IconButton></div>
      </div>
      {h?.Enabled ? <dl className="assistant-info-facts assistant-grant-facts">
        <div><dt>{zh ? '同时在职' : 'Active limit'}</dt><dd>{zh ? `最多 ${h.MaxAssistants} 位` : `Up to ${h.MaxAssistants} assistants`}</dd></div>
        <div><dt>{zh ? '共享总预算' : 'Shared budget'}</dt><dd>{currencyText(h.Budget, locale)}</dd></div>
        <div><dt>{zh ? '有效期' : 'Validity'}</dt><dd><PermissionExpiry value={h.ExpiresAt} /></dd></div>
      </dl> : <p className="assistant-info-help assistant-grant-note">{zh ? '设置数量、预算与附带权限后，助理即可雇佣临时助理。' : 'Set a limit, budget and permissions to allow temporary hiring.'}</p>}
    </article>
    <AssistantPeerCommunication assistant={value} ready={!!current.data} /></div>
    <ModalPresence>{editing && <HiringEditor assistant={editing} models={models} onClose={() => setEditing(null)} />}</ModalPresence>
  </section>;
}

export function HiringEditor({ assistant, models, onClose, request }: { assistant: Assistant; models: Model[]; onClose: () => void; request?: PermissionRequest<DelegationPermissionRequestInput> }) {
  const { t, locale } = useCopy(), zh = locale === 'zh', action = useAction();
  const [baseline] = useState(assistant);
  const formID = useId();
  const proposal = request?.Input.hiring;
  const h = proposal ? { Enabled: true, MaxAssistants: proposal.max_assistants, Budget: proposal.budget, GrantIDs: proposal.grant_ids, ExpiresAt: request!.Input.expires_at } : baseline.Hiring;
  const [enabled, setEnabled] = useState(h?.Enabled ?? false);
  const [count, setCount] = useState(String(h?.MaxAssistants ?? 3));
  const [budget, setBudget] = useState(h ? String(h.Budget.Micros / 1e6) : '');
  const [currency, setCurrency] = useState<CurrencyCode>((h?.Budget.Currency as CurrencyCode) ?? 'USD');
  const [until, setUntil] = useState<string | null>(h?.ExpiresAt ?? null);
  const [ids, setIDs] = useState<string[]>(h?.GrantIDs ?? []);
  const grants = useQuery<Grant[]>(`/api/grants?assistant_id=${assistant.ID}`, true);
  const resources = useQuery<Resource[]>('/api/resources', true);
  const save = useMutation([assistant.ID, baseline.Version, request?.ID, request?.Version, enabled, count, budget, currency, until, ids]);
  useErrorToast(action.error); useErrorToast(grants.error, grants.reload); useErrorToast(resources.error, resources.reload);
  const choices = (grants.data ?? []).filter(g => !g.RevokedAt && (!g.ExpiresAt || Date.parse(g.ExpiresAt) > Date.now()) && !g.Scope.FullAccess && !g.Scope.Once && ['model', 'storage', 'compute'].includes(resources.data?.find(r => r.ID === g.ResourceID)?.Kind ?? ''));
  return <Dialog open title={request ? (zh ? '审核授权' : 'Review permission') : (zh ? '雇佣临时助理授权' : 'Temporary hiring authorization')} description={assistant.Name} dismissible={!action.busy} onOpenChange={open => { if (!open && !action.busy) onClose(); }} footer={<>
    <Button variant="secondary" disabled={action.busy} onClick={onClose}>{t.cancel}</Button>
    <Button type="submit" form={formID} loading={action.busy} disabled={(enabled && !!until && Date.parse(until) <= Date.now()) || !(Number(budget) > 0) || (enabled && !choices.some(g => ids.includes(g.ID) && !!g.Scope.ModelID))}>{request ? (zh ? '批准授权' : 'Approve permission') : t.save}</Button>
  </>}><form id={formID} className="editor" onSubmit={event => { event.preventDefault(); void action.run(async () => {
    if (request) await save('/api/permission-requests/resolve', { RequestID: request.ID, Decision: 'approve', AssistantVersion: baseline.Version, Hiring: { max_assistants: Number(count), budget: { Currency: currency, Micros: micros(budget) }, grant_ids: ids }, ExpiresAt: until }, request.Version);
    else await save<Assistant>('/api/assistants/hiring', { AssistantID: assistant.ID, Authorization: { Enabled: enabled, MaxAssistants: Number(count), Budget: { Currency: currency, Micros: micros(budget) }, ExpiresAt: until, GrantIDs: ids } }, baseline.Version);
  }, onClose); }}>
    {request && <DelegationReviewContext assistant={baseline} request={request} />}
    <fieldset disabled={action.busy || assistant.State !== 'active'} className="assistant-info-fields editor">
      {!request && <Switch label={zh ? '允许雇佣临时助理' : 'Allow temporary hiring'} checked={enabled} onChange={event => setEnabled(event.target.checked)} />}
      <TextField label={zh ? '同时在职数量上限' : 'Maximum active assistants'} type="number" min={1} max={32} required value={count} onChange={event => setCount(event.target.value)} />
      <MoneyInput label={zh ? '共享总预算（{currency}）' : 'Shared total budget ({currency})'} value={budget} currency={currency} onChange={setBudget} onCurrencyChange={setCurrency} />
      <p className="muted">{zh ? '所有临时助理共同使用；结束雇佣后已用金额仍计入。' : 'Shared by all temporary assistants, including amounts spent by ended workers.'}</p>
      <DateTimePicker className="permission-expiry-picker" size="md" label={t.resources.expires} value={until} onValueChange={setUntil} min={new Date().toISOString()} disabled={action.busy || assistant.State !== 'active'} />
      <h4>{zh ? '可授予临时助理的权限' : 'Permissions available to temporary assistants'}</h4>
      <p className="muted">{zh ? '雇主可在雇佣时从所选权限中选择一部分；未指定时使用全部，不能超出此范围。创建后不能追加权限，原授权到期或撤销后不再有效。' : 'The employer may choose a subset when hiring. If omitted, all selected permissions apply; grants outside this set are unavailable. Permissions cannot be added after creation and stop applying when the source grant expires or is revoked.'}</p>
      {choices.map(g => <Checkbox key={g.ID} label={grantOptionLabel(t, g, resources.data?.find(r => r.ID === g.ResourceID), models)} checked={ids.includes(g.ID)} onChange={event => setIDs(old => event.target.checked ? [...old, g.ID] : old.filter(id => id !== g.ID))} />)}
      {!choices.length && <p className="muted">{zh ? '请先为该助理配置可附带的资源授权。' : 'Configure resource grants for this assistant first.'}</p>}
    </fieldset>
  </form></Dialog>;
}
