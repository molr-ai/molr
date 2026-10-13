import { AssistantContextField, clampContextTokens } from './AssistantContextField';
import { useErrorToast } from '../../../error-toasts';
import { AssistantAvatarField } from './AssistantAvatarField';
import { AssistantBudgetFields, AssistantBudgetUsage } from './AssistantBudgetFields';
import { CurrencyDialog } from '../components/Money';
import { useEffect, useState } from 'react';
import { Checkbox, Select, TextField } from '@molr/ui';
import { api, APIError } from '../../../api/client';
import type { Assistant, AssistantBudget, CurrencyCode, CurrencyContext, Grant, Model, Resource } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { grantOptionLabel } from '../../../resource-permissions';
import { ModelSelect } from '../components/ModelSelect';
import { EditDialog, EditForm, micros, useAction } from '../components/common';

type HireProps = { models: Model[]; defaultModelID?: string; onClose: () => void; onCreated: (id: string) => void };
export function HireAssistant(props: HireProps) {
  const { t } = useCopy();
  return <CurrencyDialog title={t.assistants.hire} onClose={props.onClose}>{context => <HireForm {...props} currencyContext={context} />}</CurrencyDialog>;
}
function HireForm({ models, defaultModelID, onClose, onCreated, currencyContext }: HireProps & { currencyContext: CurrencyContext }) {
  const { t } = useCopy(), [name, setName] = useState(''), [model, setModel] = useState(models.find(model => model.ID === defaultModelID && model.SupportsTools)?.ID ?? models.find(model => model.SupportsTools)?.ID ?? '');
  const [contextTokens, setContextTokens] = useState(0);
  const [avatar, setAvatar] = useState(''), [avatarBusy, setAvatarBusy] = useState(false);
  const [currency, setCurrency] = useState<CurrencyCode>(currencyContext.Currency);
  const [limit, setLimit] = useState(''), [period, setPeriod] = useState<AssistantBudget['Period']>('daily');
  return <EditDialog submitDisabled={avatarBusy} title={t.assistants.hire} onClose={onClose} submitLabel={t.assistants.hire} onSubmit={async () => {
    if (!model) throw new APIError('failed_precondition');
    const assistant = await api.mutate<Assistant>('/api/assistants', { Name: name.trim(), Avatar: avatar, ModelID: model, ContextCompressionTokens: clampContextTokens(contextTokens, models.find(item => item.ID === model)), Budget: { Limit: { Currency: currency, Micros: micros(limit) }, Period: period } }); onCreated(assistant.ID);
  }}><AssistantAvatarField name={name} value={avatar} onChange={setAvatar} onBusyChange={setAvatarBusy} /><TextField label={t.assistants.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /><ModelSelect models={models} value={model} onValueChange={id => { setModel(id); setContextTokens(value => clampContextTokens(value, models.find(item => item.ID === id))); }} /><p className="muted">{t.chat.modelAccess}</p><AssistantContextField model={models.find(item => item.ID === model)} value={contextTokens} onChange={setContextTokens} /><AssistantBudgetFields currency={currency} onCurrencyChange={setCurrency} context={currencyContext} limit={limit} period={period} onLimitChange={setLimit} onPeriodChange={setPeriod} /></EditDialog>;
}
type EditProps = { assistant: Assistant; models: Model[]; onClose: () => void };
export function AssistantSettingsForm({ assistant, models, onClose, currencyContext, action }: EditProps & { currencyContext: CurrencyContext; action: ReturnType<typeof useAction> }) {
  const { t } = useCopy(), [name, setName] = useState(assistant.Name), [model, setModel] = useState(assistant.ModelID);
  const [contextTokens, setContextTokens] = useState(assistant.ContextCompressionTokens ?? 0);
  const [avatar, setAvatar] = useState(assistant.Avatar ?? ''), [avatarBusy, setAvatarBusy] = useState(false);
  const [currency, setCurrency] = useState<CurrencyCode>((assistant.Budget?.Limit.Currency as CurrencyCode) || currencyContext.Currency);
  const [limit, setLimit] = useState(assistant.Budget?.Period ? String(assistant.Budget.Limit.Micros / 1_000_000) : ''), [period, setPeriod] = useState<AssistantBudget['Period']>(assistant.Budget?.Period || 'daily');
  const [baseline, setBaseline] = useState(assistant);
  const pristine = contextTokens === (baseline.ContextCompressionTokens ?? 0) && name === baseline.Name && avatar === (baseline.Avatar ?? '') && model === baseline.ModelID && currency === (baseline.Budget?.Limit.Currency || currencyContext.Currency) && limit === (baseline.Budget?.Period ? String(baseline.Budget.Limit.Micros / 1_000_000) : '') && period === (baseline.Budget?.Period || 'daily');
  useEffect(() => {
    if (!pristine || avatarBusy || action.busy || assistant.Version === baseline.Version) return;
    setContextTokens(assistant.ContextCompressionTokens ?? 0); setName(assistant.Name); setAvatar(assistant.Avatar ?? ''); setModel(assistant.ModelID);
    setCurrency((assistant.Budget?.Limit.Currency as CurrencyCode) || currencyContext.Currency);
    setLimit(assistant.Budget?.Period ? String(assistant.Budget.Limit.Micros / 1_000_000) : ''); setPeriod(assistant.Budget?.Period || 'daily');
    setBaseline(assistant);
  }, [assistant, baseline.Version, pristine, avatarBusy, action.busy, currencyContext.Currency]);
  return <EditForm submitDisabled={avatarBusy} action={action} onClose={onClose} onSubmit={() => api.mutate('/api/assistants/update', { ID: assistant.ID, Name: name.trim(), Avatar: avatar, ModelID: model, ContextCompressionTokens: clampContextTokens(contextTokens, models.find(item => item.ID === model)), Budget: { Limit: { Currency: currency, Micros: micros(limit) }, Period: period } }, baseline.Version)}>
    <AssistantAvatarField name={name} value={avatar} onChange={setAvatar} onBusyChange={setAvatarBusy} />
    <TextField label={t.assistants.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /><ModelSelect models={models} value={model} onValueChange={id => { setModel(id); setContextTokens(value => clampContextTokens(value, models.find(item => item.ID === id))); }} showProvider />
    <AssistantContextField model={models.find(item => item.ID === model)} value={contextTokens} onChange={setContextTokens} />
    <AssistantBudgetFields currency={currency} onCurrencyChange={setCurrency} context={currencyContext} limit={limit} period={period} onLimitChange={setLimit} onPeriodChange={setPeriod} />
    {assistant.Budget?.Period && <AssistantBudgetUsage assistantID={assistant.ID} />}
  </EditForm>;
}
export function CloneAssistant({ assistant, onClose, onCreated }: { assistant: Assistant; onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useCopy(), [name, setName] = useState(assistant.Name), [history, setHistory] = useState(true), [memory, setMemory] = useState(true), [grants, setGrants] = useState<string[]>([]);
  const resources = useQuery<Resource[]>('/api/resources', true);
  const models = useQuery<Model[]>('/api/models');
  const query = useQuery<Grant[]>(`/api/grants?assistant_id=${assistant.ID}`, true);
  useErrorToast(query.error || resources.error || models.error, () => { query.reload(); resources.reload(); models.reload(); });
  return <EditDialog title={t.assistants.clone} onClose={onClose} submitLabel={t.confirm} onSubmit={async () => { const clone = await api.mutate<Assistant>('/api/assistants/clone', { SourceID: assistant.ID, Name: name.trim(), CopyMessages: history, CopyMemory: memory, GrantIDs: grants }); onCreated(clone.ID); }}>
    <p>{t.assistants.cloneHelp}</p><TextField label={t.assistants.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /><Checkbox label={t.chat.message} checked={history} onChange={event => setHistory(event.target.checked)} /><Checkbox label={t.settings.memory} checked={memory} onChange={event => setMemory(event.target.checked)} />
    <Select multiple label={t.assistants.permissions} value={grants} onValueChange={setGrants} options={(query.data ?? []).filter(item => !item.RevokedAt && (!item.ExpiresAt || Date.parse(item.ExpiresAt) > Date.now())).map(item => { const resource = resources.data?.find(row => row.ID === item.ResourceID); return { value: item.ID, label: grantOptionLabel(t, item, resource, models.data ?? []) }; })} />
  </EditDialog>;
}
