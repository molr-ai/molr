import { useId, useState } from 'react';
import { Alert, Button, Dialog, SearchField, SecretField, SegmentedControl, Select, TextField, useToast } from '@molr/ui';
import { Cloud, Monitor, Settings2 } from 'lucide-react';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { api, APIError } from '../../../api/client';
import type { Resource } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { LocalProviderDiscovery } from './LocalProviderDiscovery';
import { ErrorNotice, Loading, useAction } from '../components/common';

// Only implemented authentication methods appear in the UI. OAuth remains a
// future provider capability, not a generic authentication option.
type AuthenticationMethod = 'api-key' | 'none' | 'oauth';
const providers = [
  { id: 'openai', name: 'OpenAI', category: 'cloud', aliases: '', endpoint: 'https://api.openai.com/v1', icon: Cloud, methods: ['api-key'] },
  { id: 'deepseek', name: 'DeepSeek', category: 'cloud', aliases: '深度求索', endpoint: 'https://api.deepseek.com/v1', icon: Cloud, methods: ['api-key'] },
  { id: 'bailian', name: '', category: 'cloud', aliases: '百炼 阿里云 通义千问 Alibaba Cloud Bailian DashScope Qwen', endpoint: 'https://dashscope.aliyuncs.com/compatible-mode/v1', icon: Cloud, methods: ['api-key'] },
  { id: 'zhipu', name: '', category: 'cloud', aliases: '智谱 智谱清言 Zhipu BigModel GLM', endpoint: 'https://open.bigmodel.cn/api/paas/v4', icon: Cloud, methods: ['api-key'] },
  { id: 'moonshot', name: '', category: 'cloud', aliases: '月之暗面 Moonshot Kimi', endpoint: 'https://api.moonshot.cn/v1', icon: Cloud, methods: ['api-key'] },
  { id: 'ollama', name: 'Ollama', category: 'local', aliases: '', endpoint: 'http://127.0.0.1:11434/v1', icon: Monitor, methods: ['none', 'api-key'] },
  { id: 'llamacpp', name: 'llama.cpp', category: 'local', aliases: '', endpoint: 'http://127.0.0.1:8080/v1', icon: Monitor, methods: ['none', 'api-key'] },
  { id: 'vllm', name: 'vLLM', category: 'local', aliases: '', endpoint: 'http://127.0.0.1:8000/v1', icon: Monitor, methods: ['none', 'api-key'] },
  { id: 'sglang', name: 'SGLang', category: 'local', aliases: '', endpoint: 'http://127.0.0.1:30000/v1', icon: Monitor, methods: ['none', 'api-key'] },
  { id: 'custom', name: '', category: 'any', aliases: '自定义 Custom', endpoint: '', icon: Settings2, methods: ['none', 'api-key'] },
] as const;
type ProviderFilter = 'all' | 'cloud' | 'local';
type ProviderID = typeof providers[number]['id'];
type Connection = { Resource: Resource; Endpoint: string; CredentialID: string; PricingProvider?: ProviderID };
type Props = { inline?: boolean; resourceID?: string; onConnected: (resource: Resource) => void; onClose: () => void };
type Draft = { name: string; endpoint: string; authentication: AuthenticationMethod; apiKey: string; replaceKey: boolean };
const baseURL = (value: string) => value.replace(/\/chat\/completions\/?$/, '').replace(/\/$/, '');
function newDraft(id: ProviderID): Draft {
  const provider = providers.find(provider => provider.id === id)!;
  return { name: '', endpoint: provider.endpoint, authentication: provider.methods[0], apiKey: '', replaceKey: true };
}

export function ProviderConnectionDialog({ inline, resourceID, onConnected, onClose }: Props) {
  const { t } = useCopy(), connection = useQuery<Connection>(resourceID ? `/api/resources/${resourceID}/connection` : null);
  if (resourceID && !connection.data) return <Dialog open title={t.resources.editProvider} onOpenChange={open => { if (!open) onClose(); }}>
    <ErrorNotice error={connection.error} retry={connection.reload} />{connection.loading && <Loading />}
  </Dialog>;
  return <ProviderForm inline={inline} initial={connection.data ?? undefined} onConnected={onConnected} onClose={onClose} />;
}

function ProviderForm({ inline, initial, onConnected, onClose }: Omit<Props, 'resourceID'> & { initial?: Connection }) {
  const { t } = useCopy(), action = useAction(), testAction = useAction(), { toast } = useToast();
  const verification = useReauthentication();
  const [tested, setTested] = useState<{ ModelCount: number } | null>(null);
  const busy = action.busy || testAction.busy;
  const initialProvider = initial ? initial.PricingProvider || providers.find(item => item.endpoint && item.endpoint === baseURL(initial.Endpoint))?.id || 'custom' : 'openai';
  const [search, setSearch] = useState('');
  const [providerFilter, setProviderFilter] = useState<ProviderFilter>('all');
  const [provider, setProvider] = useState<ProviderID>(initialProvider);
  const [drafts, setDrafts] = useState<Partial<Record<ProviderID, Draft>>>(() => initial ? { [initialProvider]: {
    name: initial.Resource.Name, endpoint: baseURL(initial.Endpoint), authentication: initial.CredentialID ? 'api-key' : 'none', apiKey: '', replaceKey: !initial.CredentialID,
  } } : {});
  const draft = drafts[provider] ?? newDraft(provider);
  const formID = useId();
  const selected = providers.find(item => item.id === provider)!;
  const nameOf = (item: typeof providers[number]) => item.name || t.resources.providerNames[item.id as keyof typeof t.resources.providerNames];
  const providerName = nameOf(selected);
  const query = search.trim().toLocaleLowerCase();
  // Custom endpoints can connect either a cloud service or a local server.
  const visibleProviders = providers.filter(item => (providerFilter === 'all' || item.category === providerFilter || item.category === 'any')
    && `${nameOf(item)} ${item.id} ${item.aliases}`.toLocaleLowerCase().includes(query));
  const storedKey = !!initial?.CredentialID && !draft.replaceKey;
  function update(patch: Partial<Draft>) {
    setTested(null); testAction.reset(); action.reset();
    setDrafts(values => ({ ...values, [provider]: { ...(values[provider] ?? newDraft(provider)), ...patch } }));
  }
  function endpoint() {
    const url = new URL(draft.endpoint.trim() || selected.endpoint);
    if (!url.pathname.endsWith('/chat/completions')) url.pathname = `${url.pathname.replace(/\/$/, '')}/chat/completions`;
    return { Endpoint: url.toString(), AllowedOrigins: [url.origin] };
  }
  async function testConnection() {
    setTested(null);
    if (draft.authentication === 'oauth') throw new APIError('failed_precondition');
    const result = await api.post<{ ModelCount: number }>('/api/resources/test-connection', { Provider: 'openai-compatible',
      Binding: { ...endpoint(), CredentialID: draft.authentication === 'api-key' && storedKey ? initial!.CredentialID : '' },
      APIKey: draft.authentication === 'api-key' && !storedKey ? draft.apiKey.trim() : '',
    });
    setTested(result);
  }
  async function submit() {
    const binding = endpoint();
    if (draft.authentication === 'oauth') throw new APIError('failed_precondition');
    const newKey = draft.authentication === 'api-key' && !storedKey;
    const proof = newKey ? await verification.verify('credential.put') : '';
    let resource: Resource;
    try {
      resource = await api.mutate<Resource>('/api/resources/provider', {
        ID: initial?.Resource.ID ?? '', Name: draft.name.trim() || providerName, PricingProvider: provider, Endpoint: binding.Endpoint,
        UseAPIKey: draft.authentication === 'api-key', APIKey: newKey ? draft.apiKey.trim() : '', verification: proof,
      }, initial?.Resource.Version ?? 0);
    } finally { update({ apiKey: '' }); }
    if (!initial || binding.Endpoint !== initial.Endpoint || newKey || (draft.authentication === 'api-key') !== !!initial.CredentialID) {
      try {
        resource = await api.mutate<Resource>('/api/resources/refresh-models', { ID: resource.ID }, resource.Version);
        toast({ title: initial ? t.saved : t.resources.providerConnected });
      } catch { toast({ title: t.resources.providerSaved, description: t.resources.discoveryFailed, tone: 'neutral' }); }
    } else toast({ title: t.saved });
    onConnected(resource);
  }
  const endpointField = <TextField label={t.resources.endpoint} type="url" value={draft.endpoint} onChange={event => update({ endpoint: event.target.value })} required={provider === 'custom' || !!initial} placeholder="https://api.example.com/v1" />;
  const footer = <><Button variant="secondary" type="submit" form={formID} value="test" loading={testAction.busy} disabled={action.busy}>{t.resources.testConnection}</Button><Button type="submit" form={formID} loading={action.busy} disabled={testAction.busy}>{initial ? t.save : t.resources.connectActions.model}</Button></>;
  const content = <>
    {!initial && <LocalProviderDiscovery disabled={busy} onSelect={item => {
      if (item.ResourceID) { void api.get<Resource>(`/api/resources/${item.ResourceID}`).then(onConnected).catch(() => {}); return; }
      const id = item.Preset as ProviderID;
      setProvider(id); setTested(null); testAction.reset(); action.reset();
      setDrafts(values => ({ ...values, [id]: { ...newDraft(id), name: item.Name === 'OpenAI compatible' ? 'Local model' : item.Name, endpoint: item.Endpoint, authentication: item.State === 'authentication_required' ? 'api-key' : 'none' } }));
    }} />}
    <div className={`provider-connect-layout${initial ? ' provider-connect-layout--editing' : ''}`}>
      {!initial && <div className="provider-connect-providers" role="group" aria-label={t.resources.providerList}>
        <p className="provider-connect-label">{t.resources.providerList}</p>
        <div className="provider-connect-filters">
          <SegmentedControl<ProviderFilter> label={t.resources.providerType} hideLabel value={providerFilter} onValueChange={setProviderFilter} disabled={busy}
            options={(['all', 'cloud', 'local'] as const).map(value => ({ value, label: t.resources.providerFilters[value] }))} />
          <SearchField label={t.resources.searchProviders} placeholder={t.resources.searchProviders} hideLabel value={search} onValueChange={setSearch} disabled={busy} />
        </div>
        <div className="provider-connect-provider-options">{visibleProviders.map(item => {
          const Icon = item.icon;
          return <button type="button" className="provider-connect-provider" key={item.id} disabled={busy} aria-pressed={provider === item.id}
            onClick={() => { setProvider(item.id); setTested(null); testAction.reset(); action.reset(); }}><Icon size={18} aria-hidden="true" /><span>{nameOf(item)}</span></button>;
        })}</div>
        {visibleProviders.length === 0 && <p className="provider-connect-empty muted" role="status">{t.resources.noMatchingProviders}</p>}
      </div>}
      <form id={formID} className="provider-connect-form" aria-label={t.resources.providerConfiguration} onSubmit={event => { event.preventDefault();
        if (((event.nativeEvent as SubmitEvent).submitter as HTMLButtonElement | null)?.value === 'test') void testAction.run(testConnection, undefined, false);
        else void action.run(submit, onClose, false); }}>
        <header><h3>{initial ? initial.Resource.Name : providerName}</h3><p className="muted">{selected.category === 'local' ? t.resources.localProviderHelp.replace('{provider}', providerName) : t.resources.modelProviderHelp}</p>
          {selected.category !== 'local' && <Alert tone="warning" title={t.resources.privacyTitle}>{t.resources.providerPrivacy}</Alert>}
        </header>
        <fieldset disabled={busy} className="provider-connect-fields">
          {initial && <TextField label={t.name} value={draft.name} onChange={event => update({ name: event.target.value })} required maxLength={256} />}
          {(initial || provider === 'custom') && endpointField}
          {selected.methods.length > 1 && <Select label={t.resources.authentication} value={draft.authentication} disabled={busy} onValueChange={authentication => update({ authentication })}
            options={selected.methods.map(value => ({ value, label: t.resources.authMethods[value] }))} />}
          {draft.authentication === 'api-key' && (storedKey ? <div className="provider-key-status"><span>{t.resources.keyConfigured}</span><Button variant="secondary" size="sm" onClick={() => update({ replaceKey: true })}>{t.resources.replaceKey}</Button></div> : <>
            <SecretField key={provider} name={`provider-${provider}-${initial?.Resource.ID ?? 'new'}-api-key`} label={t.settings.apiKey} value={draft.apiKey} onChange={event => update({ apiKey: event.target.value })} required />
            {initial?.CredentialID && <Button variant="ghost" size="sm" onClick={() => update({ replaceKey: false, apiKey: '' })}>{t.resources.keepKey}</Button>}
          </>)}
          {!initial && <details key={provider} className="provider-connect-more"><summary>{t.resources.moreConfiguration}</summary><div className="provider-connect-fields">
            <TextField label={t.name} value={draft.name} onChange={event => update({ name: event.target.value })} placeholder={providerName} maxLength={256} />
            {provider !== 'custom' && endpointField}
          </div></details>}
        </fieldset>
        {tested && <Alert tone="positive" title={t.resources.connectionTestSucceeded.replace('{count}', String(tested.ModelCount))} onDismiss={() => setTested(null)} />}
        <ErrorNotice error={testAction.error} /><ErrorNotice error={action.error} />
      </form>
    </div>
  </>;
  return <>{inline ? <section className="setup-provider">{content}<div className="setup-actions">{footer}</div></section> : <Dialog open title={initial ? t.resources.editProvider : t.resources.connectActions.model} size={initial ? 'md' : 'lg'} className="provider-connect-dialog" dismissible={!busy} onOpenChange={open => { if (!open && !busy) onClose(); }} footer={<><Button variant="secondary" disabled={busy} onClick={onClose}>{t.cancel}</Button>{footer}</>}>{content}</Dialog>}{verification.dialog}</>;
}
