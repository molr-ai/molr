import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Badge, Button, IconButton, useBreakpoint } from '@molr/ui';
import { ChevronLeft, Plus } from 'lucide-react';
import { api, APIError } from '@molr/sdk';
import type { CurrencyContext, Model, Resource } from '@molr/sdk';
import { useErrorToast } from '../../../error-toasts';
import { useCopy } from '../../../state';
import { Loading, useAction } from '../components/common';
import { ModelConfigurationForm } from './ModelConfigurationForm';

type Configuration = { resource: Resource; model?: Model; currencyContext: CurrencyContext };
export function ProviderModelsPanel({ resource, models, loading, actions, connectionState, onClose }: { resource: Resource; models: Model[]; loading: boolean; actions: ReactNode; connectionState: string; onClose: () => void }) {
  const { t } = useCopy(), mobile = useBreakpoint('mobile'), action = useAction();
  const [target, setTarget] = useState<{ modelID?: string } | null>(null);
  const [configuration, setConfiguration] = useState<Configuration | null>(null), [error, setError] = useState<unknown>();
  const [attempt, setAttempt] = useState(0);
  const backRef = useRef<HTMLButtonElement>(null), addRef = useRef<HTMLButtonElement>(null), triggerRef = useRef<HTMLButtonElement | null>(null), detailRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null), listScroll = useRef(0);
  const detail = target !== null;
  useEffect(() => {
    if (!target) return;
    const abort = new AbortController();
    setConfiguration(null); setError(undefined);
    void Promise.all([
      api.get<Resource>(`/api/resources/${resource.ID}`, abort.signal),
      target.modelID ? api.get<Model[]>('/api/models', abort.signal) : Promise.resolve([]),
      api.get<CurrencyContext>('/api/settings/currency', abort.signal),
    ]).then(([current, catalog, currencyContext]) => {
      if (abort.signal.aborted) return;
      const model = catalog.find(item => item.ID === target.modelID && item.ResourceID === current.ID);
      if (target.modelID && !model) throw new APIError('not_found');
      setConfiguration({ resource: current, model, currencyContext });
    }).catch(error => { if (!abort.signal.aborted) setError(error); });
    return () => abort.abort();
  }, [resource.ID, target, attempt]);
  useErrorToast(error, () => setAttempt(value => value + 1));
  useEffect(() => {
    if (detail) { detailRef.current?.scrollTo(0, 0); backRef.current?.focus({ preventScroll: true }); }
    else if (triggerRef.current) { listRef.current?.scrollTo(0, listScroll.current); triggerRef.current.focus({ preventScroll: true }); }
  }, [detail]);
  useEffect(() => { if (mobile) backRef.current?.focus({ preventScroll: true }); }, [mobile]);
  function configure(model: Model | undefined, trigger: HTMLButtonElement) {
    listScroll.current = listRef.current?.scrollTop ?? 0;
    triggerRef.current = trigger; action.reset(); setConfiguration(null); setTarget({ modelID: model?.ID });
  }
  function back() { if (!action.busy) { triggerRef.current ??= addRef.current; setError(undefined); setTarget(null); } }
  const title = detail ? target.modelID ? t.resources.configureModel : t.resources.addModel : resource.Name;
  return <section className="provider-models-panel" aria-label={title}>
    <header className="provider-models-heading">
      {(detail || mobile) && <IconButton ref={backRef} label={t.back} disabled={action.busy} onClick={detail ? back : onClose}><ChevronLeft size={18} /></IconButton>}
      <div className="provider-models-identity"><div className="provider-models-title"><h2>{title}</h2>{!detail && <Badge tone={connectionState === 'available' ? 'positive' : 'neutral'}>{t.resources[connectionState as 'available' | 'offline' | 'disconnected']}</Badge>}</div>
        {!detail && <p className="provider-models-description">{resource.Authentication === 'chatgpt' ? 'OpenAI · ChatGPT' : resource.Provider}</p>}
      </div>{!detail && <span className="provider-models-actions">{actions}</span>}
    </header>
    <div className="provider-models-pages" data-detail={detail}>
      <div ref={listRef} className="provider-models-page" inert={detail} aria-hidden={detail}>
        <div className="provider-models-toolbar"><h3>{t.resources.providerModels}</h3><Button ref={addRef} size="sm" variant="ghost" disabled={resource.State !== 'available'} onClick={event => configure(undefined, event.currentTarget)}><Plus size={14} />{t.resources.addModel}</Button></div>
        {loading ? <Loading /> : models.length ? <ul className="resource-models-list">{models.map(model => <li key={model.ID}>
          <div className="resource-model-copy"><div className="resource-model-name"><strong><button type="button" className="resource-model-open row-open" disabled={resource.State !== 'available'} onClick={event => configure(model, event.currentTarget)}>{model.Name}</button></strong>{!model.Pricing && <Badge tone="warning">{t.resources.pricingUnknown}</Badge>}</div>
            <dl className="resource-model-limits"><div><dt>{t.resources.contextWindow}</dt><dd>{(model.EffectiveContextLimit ?? 262144).toLocaleString()} tokens</dd></div><div><dt>{t.resources.outputWindow}</dt><dd>{(model.EffectiveOutputLimit ?? 32768).toLocaleString()} tokens</dd></div></dl>
          </div>
        </li>)}</ul> : <p className="muted">{t.resources.noDiscoveredModels}</p>}
      </div>
      <div ref={detailRef} className="provider-models-page" inert={!detail} aria-hidden={!detail}>
        {configuration ? <ModelConfigurationForm {...configuration} action={action} onClose={() => { triggerRef.current ??= addRef.current; setTarget(null); }} /> : !error && <Loading />}
      </div>
    </div>
  </section>;
}
