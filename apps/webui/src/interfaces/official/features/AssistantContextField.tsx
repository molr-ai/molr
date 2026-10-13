import { Button, Slider } from '@molr/ui';
import type { Model } from '../../../api/types';
import { useCopy } from '../../../state';
import { SettingsHelp } from '../components/SettingsHelp';

function bounds(model?: Model) {
  const window = model?.EffectiveContextLimit || model?.ContextLimit || model?.ProviderContextLimit || 256 * 1024;
  const max = Math.max(1, Math.floor(window * 4 / 5));
  return { min: Math.min(64 * 1024, max), max };
}
export function clampContextTokens(value: number, model?: Model) {
  if (!value) return 0;
  const { min, max } = bounds(model);
  return Math.max(min, Math.min(max, value));
}
export function AssistantContextField({ model, value, onChange }: { model?: Model; value: number; onChange: (value: number) => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const { min, max } = bounds(model), current = value ? clampContextTokens(value, model) : Math.min(128 * 1024, max);
  const label = zh ? '上下文压缩阈值' : 'Context compression threshold';
  const help = zh
    ? '上下文达到此用量后，在当前工具调用批次完成时压缩。较低的阈值可减少后续输入，但会增加压缩频率并可能丢失细节。默认取 128K 与模型窗口 80% 中的较小值；上限不超过 64K 时固定且不可调整。1K = 1024 tokens。'
    : 'Compacts context at this usage after the current tool batch finishes. Lower thresholds reduce subsequent input but increase compaction frequency and may lose detail. The default is the smaller of 128K and 80% of the model window. A maximum of 64K or less is fixed. 1K = 1024 tokens.';
  const format = (n: number) => `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(n / 1024)}K`;
  return <div className="assistant-context-field">
    <Slider label={label} labelExtra={<SettingsHelp label={label} content={help} />} min={min} max={max} step={8 * 1024} value={current} onValueChange={onChange} disabled={!model} formatValue={format} />
    <div className="assistant-context-field__mode"><span>{value === 0 ? (zh ? '自动' : 'Automatic') : (zh ? '自定义' : 'Custom')}</span><Button type="button" size="sm" variant="ghost" disabled={value === 0} onClick={() => onChange(0)}>{zh ? '恢复默认' : 'Restore default'}</Button></div>
  </div>;
}
