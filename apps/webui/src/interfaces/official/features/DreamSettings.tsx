import { useMutation } from '../../../use-mutation';
import { useEffect, useState } from 'react';
import { Switch, DateTimePicker, TextField } from '@molr/ui';
import type { DreamConfig } from '@molr/sdk';
import { useCopy } from '../../../state';

type Draft = { enabled: boolean; time: string; duration: string };
const draft = (config: DreamConfig): Draft => ({ enabled: config.Enabled, time: `${String(config.Hour).padStart(2, '0')}:${String(config.Minute).padStart(2, '0')}`, duration: String(config.MaxDurationSeconds || 3600) });

export function useDreamSettings(config?: DreamConfig, busy = false) {
  const [baseline, setBaseline] = useState(config);
  const [value, setValue] = useState<Draft>(config ? draft(config) : { enabled: false, time: '03:00', duration: '3600' });
  const pristine = !!baseline && value.enabled === baseline.Enabled && (!value.enabled || value.time === draft(baseline).time && value.duration === draft(baseline).duration);
  useEffect(() => {
    if (!config || busy || baseline && (!pristine || config.Version <= baseline.Version)) return;
    setBaseline(config); setValue(draft(config));
  }, [config, baseline, pristine, busy]);
  const saveConfig = useMutation([baseline?.AssistantID, value]);
  async function save() {
    if (!baseline || pristine) return;
    const saved = await saveConfig<DreamConfig>('/api/dreams/config', {
      AssistantID: baseline.AssistantID, Enabled: value.enabled,
      Hour: value.enabled ? Number(value.time.split(':')[0]) : baseline.Hour,
      Minute: value.enabled ? Number(value.time.split(':')[1]) : baseline.Minute,
      MaxDurationSeconds: value.enabled ? Number(value.duration) : baseline.MaxDurationSeconds,
    }, baseline.Version);
    setBaseline(saved); setValue(draft(saved));
  }
  return { value, setValue, save, ready: !!baseline };
}

export function DreamSettingsFields({ value, onChange }: { value: Draft; onChange: (value: Draft) => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  return <>
    <Switch label={zh ? '开启做梦' : 'Enable dreaming'} checked={value.enabled} onChange={event => onChange({ ...value, enabled: event.target.checked })} />
    {value.enabled && <div className="dream-timing-fields">
      <DateTimePicker label={zh ? '开始时间' : 'Start time'} mode="time" timeZone="UTC" value={`2020-01-01T${value.time}:00Z`} onValueChange={time => { if (time) onChange({ ...value, time: new Date(time).toISOString().slice(11, 16) }); }} />
      <TextField label={zh ? '做梦时长上限（秒）' : 'Maximum dream duration (seconds)'} type="number" min={1} max={3600} value={value.duration} onChange={event => onChange({ ...value, duration: event.target.value })} required />
    </div>}
  </>;
}
