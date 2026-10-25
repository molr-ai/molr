import { CurrencySelect, MoneyText } from '../components/Money';
import { useState } from 'react';
import { Switch, TextField } from '@molr/ui';
import { api, APIError } from '@molr/sdk';
import type { CurrencyCode, CurrencyContext, Model, Resource } from '@molr/sdk';
import { useCopy } from '../../../state';
import { EditForm, micros, useAction } from '../components/common';

type Props = { resource: Resource; model?: Model; onClose: () => void; currencyContext: CurrencyContext; action: ReturnType<typeof useAction> };
export function ModelConfigurationForm({ resource, model, onClose, currencyContext, action }: Props) {
  const { t, locale } = useCopy();
  const subscription = resource.Authentication === 'chatgpt';
  const [currency, setCurrency] = useState<CurrencyCode>((model?.Pricing?.Currency as CurrencyCode) || currencyContext.Currency);
  const automatic = model?.PricingInfo?.Automatic;
  const [customPricing, setCustomPricing] = useState(!automatic || model?.PricingInfo?.Source === 'manual');
  const [name, setName] = useState(model?.Name ?? '');
  const [context, setContext] = useState(model?.ContextLimit ? String(model.ContextLimit) : ''), [tools, setTools] = useState(model?.SupportsTools ?? true);
  const [outputLimit, setOutputLimit] = useState(model?.OutputLimit ? String(model.OutputLimit) : '');
  const [images, setImages] = useState(model?.SupportsImages ?? false);
  const [audio, setAudio] = useState(model?.SupportsAudio ?? false);
  const [audioPrice, setAudioPrice] = useState(model?.Pricing?.AudioInputMicrosPerMillion == null ? "" : String(model.Pricing.AudioInputMicrosPerMillion / 1_000_000));
  const [input, setInput] = useState(model?.Pricing ? String(model.Pricing.InputMicrosPerMillion / 1_000_000) : '');
  const [output, setOutput] = useState(model?.Pricing ? String(model.Pricing.OutputMicrosPerMillion / 1_000_000) : '');
  const [cached, setCached] = useState(model?.Pricing ? String(model.Pricing.CachedInputMicrosPerMillion / 1_000_000) : '');
  return <EditForm action={action} onClose={onClose} onSubmit={async () => {
    const prices = [input, output, cached], all = prices.every(value => value !== '');
    if (customPricing && prices.some(value => value !== '') && !all) throw new APIError('invalid_argument');
    await api.mutate('/api/resources/configure-model', { ID: resource.ID, Model: { Name: name.trim(), ContextLimit: Number(context), OutputLimit: Number(outputLimit), SupportsTools: tools, SupportsImages: images, SupportsAudio: audio,
      Pricing: customPricing && all ? { ...(audioPrice !== "" ? { AudioInputMicrosPerMillion: micros(audioPrice) } : {}), Currency: currency, InputMicrosPerMillion: micros(input), OutputMicrosPerMillion: micros(output), CachedInputMicrosPerMillion: micros(cached) } : null } }, resource.Version);
  }}><p className="muted">{resource.Name}</p>{subscription && <p className="muted">{locale === 'zh' ? 'ChatGPT 套餐：以下金额为内部预算参考计价，不代表 OpenAI 实际扣费。输出上限用于本地预算预留，套餐接口不接受生成 token 上限。' : 'ChatGPT plan: prices value internal budgets, not OpenAI charges. Output limits reserve local budgets; this route does not accept a generation token cap.'}</p>}<TextField label={t.resources.modelName} value={name} onChange={event => setName(event.target.value)} readOnly={!!model} required maxLength={256} /><TextField label={t.resources.context} type="number" min="256" max="2000000" step="1" value={context} placeholder={String(model?.ProviderContextLimit || 262144)} onChange={event => setContext(event.target.value)} />
    <p className="muted">{t.resources.contextHelp}</p><TextField label={t.resources.outputLimit} type="number" min="1" max="1000000" step="1" value={outputLimit} placeholder={String(model?.ProviderOutputLimit || 32768)} onChange={event => setOutputLimit(event.target.value)} /><p className="muted">{t.resources.outputLimitHelp}</p>
    <Switch label={t.resources.supportsTools} checked={tools} onChange={event => setTools(event.target.checked)} />
    <Switch label={t.resources.supportsImages} checked={images} onChange={event => setImages(event.target.checked)} />
    <Switch disabled={subscription} label={t.resources.supportsAudio} checked={audio} onChange={event => setAudio(event.target.checked)} />
    <p className="muted">{t.resources.mediaCapabilityHelp}</p>
    {automatic && <><p className="muted">{t.resources.automaticPricing}{model?.PricingInfo?.Period && <> · {model.PricingInfo.Period === 'peak' ? t.resources.peakPricing : t.resources.offPeakPricing}</>}</p>
      <Switch label={t.resources.customPricing} checked={customPricing} onChange={event => setCustomPricing(event.target.checked)} />
      {!customPricing && <>
        {(automatic.Tiers?.length ? [{ ...automatic, InputTokensAbove: 0, OutputTokensAbove: 0 }, ...automatic.Tiers] : [automatic]).map((price, index) => <div key={index}>
          {!!automatic.Tiers?.length && <p className="muted">{index === 0 ? (locale === 'zh' ? '基础档位' : 'Base tier') : [
            'InputTokensAbove' in price && price.InputTokensAbove > 0 ? `${locale === 'zh' ? '输入' : 'Input'} > ${price.InputTokensAbove.toLocaleString(locale)} tokens` : '',
            'OutputTokensAbove' in price && price.OutputTokensAbove > 0 ? `${locale === 'zh' ? '输出' : 'Output'} > ${price.OutputTokensAbove.toLocaleString(locale)} tokens` : '',
          ].filter(Boolean).join(' · ')}</p>}
          <dl className="connection-details">{[
            [t.resources.inputPrice, price.InputMicrosPerMillion],
            [t.resources.outputPrice, price.OutputMicrosPerMillion],
            [t.resources.cachedPrice, price.CachedInputMicrosPerMillion],
          ].map(([label, amount]) => <div key={String(label)}><dt>{String(label).replace('{currency}', currencyContext.Currency)}</dt><dd><MoneyText value={{ Currency: automatic.Currency, Micros: Number(amount) }} context={currencyContext} /></dd></div>)}</dl>
        </div>)}
        {!!automatic.Tiers?.length && <p className="muted">{locale === 'zh' ? '按实际输入与输出用量选择最后一个符合条件的档位，整次请求使用该档单价。' : 'The last tier matching actual input and output usage applies to the entire request.'}</p>}
      </>}</>}

    {customPricing && <><p className="muted">{t.resources.optionalPricing}</p><CurrencySelect value={currency} onChange={setCurrency} />
    <TextField label={t.resources.inputPrice.replace('{currency}', currency)} type="number" min="0" step="0.000001" value={input} onChange={event => setInput(event.target.value)} />
    <TextField label={t.resources.outputPrice.replace('{currency}', currency)} type="number" min="0" step="0.000001" value={output} onChange={event => setOutput(event.target.value)} />
    <TextField label={t.resources.cachedPrice.replace('{currency}', currency)} type="number" min="0" max={input || undefined} step="0.000001" value={cached} onChange={event => setCached(event.target.value)} />
    {audio && <TextField label={t.resources.audioInputPrice.replace("{currency}", currency)} type="number" min="0" step="0.000001" value={audioPrice} onChange={event => setAudioPrice(event.target.value)} />}
    </>}
  </EditForm>;
}
