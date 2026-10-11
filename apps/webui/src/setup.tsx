import { useErrorToast } from './error-toasts';
import { useState, type ReactNode } from 'react';
import { Button, Select, TextField } from '@molr/ui';
import type { Model, Resource, SetupState, SetupRequest } from './api/types';
import { api } from './api/client';
import { useCopy, useQuery, useSession } from './state';
import { ProviderConnectionDialog } from './interfaces/official/features/ProviderConnectionDialog';
import { Loading, useAction } from './interfaces/official/components/common';
import './setup.css';

export function SetupGate({ children }: { children: ReactNode }) {
 const { principal } = useSession();
 const status = useQuery<SetupState>(principal?.User?.Administrator ? '/api/setup' : null);
 useErrorToast(status.error, status.reload);
 if (!principal?.User?.Administrator) return children;
 if (status.error && !status.data) return <main className="session-page" />;
 if (!status.data) return <main className="session-page"><Loading /></main>;
 if (!status.data.Required) return children;
 return <Setup onComplete={id => { location.hash = `#/assistants/${id}`; status.reload(); }} />;
}
function Setup({ onComplete }: { onComplete: (id: string) => void }) {
 const { locale } = useCopy(), zh = locale === 'zh';
 const models = useQuery<Model[]>('/api/models');
 const [changingProvider, setChangingProvider] = useState(false);
 const [provider, setProvider] = useState<Resource | null>(null), [modelID, setModelID] = useState('');
 const [budget, setBudget] = useState('1'), [inputPrice, setInputPrice] = useState(''), [outputPrice, setOutputPrice] = useState('');
 const action = useAction();
 const candidates = (models.data ?? []).filter(model => model.SupportsTools && (!provider || model.ResourceID === provider.ID));
 const selected = candidates.find(model => model.ID === modelID) ?? candidates.find(model => model.Pricing) ?? candidates[0];
 const priceKnown = !!selected?.Pricing;
 useErrorToast(models.error, models.reload);
 useErrorToast(action.error);
 async function finish() {
  if (!selected) return;
  const request: SetupRequest = { ModelID: selected.ID, Language: locale, Timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', Budget: { Limit: { Currency: 'USD', Micros: Math.round(Number(budget) * 1e6) }, Period: 'daily' }, ...(priceKnown ? {} : { Pricing: { Currency: 'USD', InputMicrosPerMillion: Math.round(Number(inputPrice) * 1e6), OutputMicrosPerMillion: Math.round(Number(outputPrice) * 1e6), CachedInputMicrosPerMillion: 0 } }) };
  const result = await api.post<SetupState>('/api/setup', request);
  onComplete(result.AssistantID);
 }
 return <main className="setup-page"><div className="setup-card"><h1>{zh ? '让 Molr 开始工作' : 'Get started with Molr'}</h1><p className="muted">{zh ? '连接一个模型，其余已经为你准备好。' : 'Connect a model. Everything else is ready for you.'}</p>
  {(!selected || changingProvider) && <ProviderConnectionDialog inline onClose={() => {}} onConnected={resource => { setProvider(resource); setChangingProvider(false); setModelID(''); setInputPrice(''); setOutputPrice(''); models.reload(); }} />}
  {models.loading && <Loading />}
  {provider && !models.loading && !candidates.length && <p role="status">{zh ? '此服务没有可用的聊天模型。请检查服务模型配置，或连接其他提供商。' : 'No chat models are available. Check the service or connect another provider.'}</p>}
  {selected && <form className="setup-finish" onSubmit={event => { event.preventDefault(); void action.run(finish); }}>
   <div className="setup-actions"><h2>{zh ? '你的助理 Molr' : 'Your assistant Molr'}</h2><Button variant="ghost" onClick={() => setChangingProvider(true)}>{zh ? '更换提供商' : 'Change provider'}</Button></div>
   <Select label={zh ? '模型' : 'Model'} value={selected.ID} onValueChange={value => { setModelID(value); setInputPrice(''); setOutputPrice(''); }} options={candidates.map(model => ({ value: model.ID, label: model.Name }))} />
   <TextField label={zh ? '每日预算（USD）' : 'Daily budget (USD)'} type="number" min="0.01" step="0.01" required value={budget} onChange={event => setBudget(event.target.value)} />
   {!priceKnown && <fieldset><legend>{zh ? '补充模型价格' : 'Set model pricing'}</legend><p className="muted">{zh ? '价格未知。每百万 token 的美元价格；填写 0 表示你确认此项免费，本机服务也可能转发付费模型。' : 'Pricing is unknown. Enter USD per million tokens; 0 confirms free use. Local services may proxy paid models.'}</p>
    <TextField label={zh ? '输入价格' : 'Input price'} type="number" min="0" step="0.000001" required value={inputPrice} onChange={event => setInputPrice(event.target.value)} />
    <TextField label={zh ? '输出价格' : 'Output price'} type="number" min="0" step="0.000001" required value={outputPrice} onChange={event => setOutputPrice(event.target.value)} />
   </fieldset>}
   <p className="muted">{zh ? '开始后 Molr 会使用所选模型向你问候，消耗计入预算。模型的实际工具调用能力以执行结果为准。文件、命令和网络权限可在需要时授予。' : 'Molr will greet you using this model; usage counts toward the budget. Tool support is confirmed during execution. Grant file, command and network permissions when needed.'}</p>
   <Button type="submit" loading={action.busy}>{zh ? '开始使用' : 'Start using Molr'}</Button>
  </form>}
 </div></main>;
}
