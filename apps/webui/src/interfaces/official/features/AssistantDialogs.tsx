import { useMutation } from '../../../use-mutation';
import { AssistantContextField, clampContextTokens } from './AssistantContextField';
import { useErrorToast } from '../../../error-toasts';
import { AssistantAvatarField } from './AssistantAvatarField';
import { AssistantBudgetFields } from './AssistantBudgetFields';
import { DreamSettingsFields, useDreamSettings } from './DreamSettings';
import { CurrencyDialog } from '../components/Money';
import { useEffect, useState } from 'react';
import { Checkbox, Select, TextField } from '@molr/ui';
import { APIError } from '@molr/sdk';
import type { Assistant, AssistantBudget, CurrencyCode, CurrencyContext, DreamConfig, Grant, Model, Resource } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { grantOptionLabel } from '../../../resource-permissions';
import { ModelSelect } from '../components/ModelSelect';
import { EditDialog, EditForm, Loading, micros, useAction } from '../components/common';

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
  const hire = useMutation([name, avatar, model, contextTokens, currency, limit, period, clampContextTokens(contextTokens, models.find(item => item.ID === model))]);
  return <EditDialog submitDisabled={avatarBusy} title={t.assistants.hire} onClose={onClose} submitLabel={t.assistants.hire} onSubmit={async () => {
    if (!model) throw new APIError('failed_precondition');
    const assistant = await hire<Assistant>('/api/assistants', { Name: name.trim(), Avatar: avatar, ModelID: model, ContextCompressionTokens: clampContextTokens(contextTokens, models.find(item => item.ID === model)), Budget: { Limit: { Currency: currency, Micros: micros(limit) }, Period: period } }); onCreated(assistant.ID);
  }}><AssistantAvatarField name={name} value={avatar} onChange={setAvatar} onBusyChange={setAvatarBusy} /><TextField label={t.assistants.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /><ModelSelect models={models} value={model} onValueChange={id => { setModel(id); setContextTokens(value => clampContextTokens(value, models.find(item => item.ID === id))); }} /><p className="muted">{t.chat.modelAccess}</p><AssistantContextField model={models.find(item => item.ID === model)} value={contextTokens} onChange={setContextTokens} /><AssistantBudgetFields currency={currency} onCurrencyChange={setCurrency} limit={limit} period={period} onLimitChange={setLimit} onPeriodChange={setPeriod} /></EditDialog>;
}
type EditProps = { assistant: Assistant; models: Model[]; onClose: () => void };
export function AssistantSettingsForm({ assistant, models, onClose, currencyContext, action, formID, onSubmitDisabledChange }: EditProps & { currencyContext: CurrencyContext; action: ReturnType<typeof useAction>; formID: string; onSubmitDisabledChange: (disabled: boolean) => void }) {
  const { t, locale } = useCopy(), [name, setName] = useState(assistant.Name), [model, setModel] = useState(assistant.ModelID);
  const dreamConfig = useQuery<DreamConfig>(`/api/dreams/config?assistant_id=${assistant.ID}`);
  const dream = useDreamSettings(dreamConfig.data, action.busy);
  useErrorToast(dreamConfig.error, dreamConfig.reload);
  const [contextTokens, setContextTokens] = useState(assistant.ContextCompressionTokens ?? 0);
  const [avatar, setAvatar] = useState(assistant.Avatar ?? ''), [avatarBusy, setAvatarBusy] = useState(false);
  const [currency, setCurrency] = useState<CurrencyCode>((assistant.Budget?.Limit.Currency as CurrencyCode) || currencyContext.Currency);
  const [limit, setLimit] = useState(assistant.Budget?.Period ? String(assistant.Budget.Limit.Micros / 1_000_000) : ''), [period, setPeriod] = useState<AssistantBudget['Period']>(assistant.Budget?.Period || 'daily');
  const [baseline, setBaseline] = useState(assistant);
  const submitDisabled = avatarBusy || !dream.ready;
  useEffect(() => { onSubmitDisabledChange(submitDisabled); }, [submitDisabled, onSubmitDisabledChange]);
  const pristine = contextTokens === (baseline.ContextCompressionTokens ?? 0) && name === baseline.Name && avatar === (baseline.Avatar ?? '') && model === baseline.ModelID && currency === (baseline.Budget?.Limit.Currency || currencyContext.Currency) && limit === (baseline.Budget?.Period ? String(baseline.Budget.Limit.Micros / 1_000_000) : '') && period === (baseline.Budget?.Period || 'daily');
  useEffect(() => {
    if (!pristine || avatarBusy || action.busy || assistant.Version <= baseline.Version) return;
    setContextTokens(assistant.ContextCompressionTokens ?? 0); setName(assistant.Name); setAvatar(assistant.Avatar ?? ''); setModel(assistant.ModelID);
    setCurrency((assistant.Budget?.Limit.Currency as CurrencyCode) || currencyContext.Currency);
    setLimit(assistant.Budget?.Period ? String(assistant.Budget.Limit.Micros / 1_000_000) : ''); setPeriod(assistant.Budget?.Period || 'daily');
    setBaseline(assistant);
  }, [assistant, baseline.Version, pristine, avatarBusy, action.busy, currencyContext.Currency]);
  const saveAssistant = useMutation([assistant.ID, name, avatar, model, contextTokens, currency, limit, period, clampContextTokens(contextTokens, models.find(item => item.ID === model))]);
  return <EditForm id={formID} externalActions submitDisabled={submitDisabled} action={action} onClose={onClose} onSubmit={async () => {
    if (!pristine) {
      const saved = await saveAssistant<Assistant>('/api/assistants/update', { ID: assistant.ID, Name: name.trim(), Avatar: avatar, ModelID: model, ContextCompressionTokens: clampContextTokens(contextTokens, models.find(item => item.ID === model)), Budget: { Limit: { Currency: currency, Micros: micros(limit) }, Period: period } }, baseline.Version);
      // Retain a successful save's version if the dream update needs a retry.
      setBaseline(saved);
    }
    await dream.save();
  }}>
    <section className="assistant-settings-section" aria-label={locale === 'zh' ? '基本信息' : 'Profile'}>
      <h3>{locale === 'zh' ? '基本信息' : 'Profile'}</h3>
      <AssistantAvatarField name={name} value={avatar} onChange={setAvatar} onBusyChange={setAvatarBusy} />
      <TextField label={t.assistants.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} />
    </section>
    <section className="assistant-settings-section" aria-label={locale === 'zh' ? '模型与上下文' : 'Model and context'}>
      <h3>{locale === 'zh' ? '模型与上下文' : 'Model and context'}</h3>
      <ModelSelect models={models} value={model} onValueChange={id => { setModel(id); setContextTokens(value => clampContextTokens(value, models.find(item => item.ID === id))); }} showProvider />
      <AssistantContextField model={models.find(item => item.ID === model)} value={contextTokens} onChange={setContextTokens} />
    </section>
    <section className="assistant-settings-section" aria-label={locale === 'zh' ? '消耗设置' : 'Spending'}>
      <h3>{locale === 'zh' ? '消耗设置' : 'Spending'}</h3>
      <AssistantBudgetFields currency={currency} onCurrencyChange={setCurrency} limit={limit} period={period} onLimitChange={setLimit} onPeriodChange={setPeriod} />
    </section>
    <section className="assistant-settings-section assistant-dream-settings" aria-label={locale === 'zh' ? '做梦设置' : 'Dream settings'}>
      <h3>{locale === 'zh' ? '做梦设置' : 'Dream settings'}</h3>
      {dream.ready ? <DreamSettingsFields value={dream.value} onChange={dream.setValue} /> : <Loading />}
    </section>
  </EditForm>;
}
export function CloneAssistant({ assistant, onClose, onCreated }: { assistant: Assistant; onClose: () => void; onCreated: (id: string) => void }) {
  const { t } = useCopy(), [name, setName] = useState(assistant.Name), [history, setHistory] = useState(true), [memory, setMemory] = useState(true), [grants, setGrants] = useState<string[]>([]);
  const resources = useQuery<Resource[]>('/api/resources', true);
  const models = useQuery<Model[]>('/api/models');
  const query = useQuery<Grant[]>(`/api/grants?assistant_id=${assistant.ID}`, true);
  useErrorToast(query.error || resources.error || models.error, () => { query.reload(); resources.reload(); models.reload(); });
  const cloneAssistant = useMutation([assistant.ID, name, history, memory, grants]);
  return <EditDialog title={t.assistants.clone} onClose={onClose} submitLabel={t.confirm} onSubmit={async () => { const clone = await cloneAssistant<Assistant>('/api/assistants/clone', { SourceID: assistant.ID, Name: name.trim(), CopyMessages: history, CopyMemory: memory, GrantIDs: grants }); onCreated(clone.ID); }}>
    <p>{t.assistants.cloneHelp}</p><TextField label={t.assistants.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /><Checkbox label={t.chat.message} checked={history} onChange={event => setHistory(event.target.checked)} /><Checkbox label={t.settings.memory} checked={memory} onChange={event => setMemory(event.target.checked)} />
    <Select multiple label={t.assistants.permissions} value={grants} onValueChange={setGrants} options={(query.data ?? []).filter(item => !item.RevokedAt && (!item.ExpiresAt || Date.parse(item.ExpiresAt) > Date.now())).map(item => { const resource = resources.data?.find(row => row.ID === item.ResourceID); return { value: item.ID, label: grantOptionLabel(t, item, resource, models.data ?? []) }; })} />
  </EditDialog>;
}
