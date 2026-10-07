import { timezoneLabel } from '../../../currency';
import { useMemo } from 'react';
import { Select } from '@molr/ui';
import type { RuntimeInfo, Settings } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { ErrorNotice, Loading } from '../components/common';
import { SettingsSwitch } from '../components/SettingsHelp';

import { useSettingsChange } from './use-settings-change';

const commonZones = ['UTC', 'Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Taipei', 'Asia/Tokyo', 'Asia/Seoul', 'Asia/Singapore', 'Asia/Dubai', 'Europe/London', 'Europe/Paris', 'Europe/Berlin', 'America/New_York', 'America/Chicago', 'America/Los_Angeles', 'Australia/Sydney', 'Pacific/Auckland'];

export function GeneralSettings() {
  const query = useQuery<Settings>('/api/settings');
  return <><ErrorNotice error={query.error} retry={query.reload} />{query.data ? <GeneralControls settings={query.data} refresh={query.reload} /> : query.loading && <Loading />}</>;
}
function GeneralControls({ settings, refresh }: { settings: Settings; refresh: () => void }) {
  const { t, locale } = useCopy();
  const { value, change, action } = useSettingsChange(settings);
  const runtime = useQuery<RuntimeInfo>('/api/status');
  const timezones = useMemo(() => {
    let supported: string[] = [];
    try { supported = Intl.supportedValuesOf('timeZone'); } catch { /* Older browsers retain common zones and the saved value. */ }
    const names: Record<string, string> = t.settings.timezoneNames;
    return [...new Set([...commonZones, settings.Timezone, ...supported])].filter(Boolean).map(zone => {
      return { value: zone, label: timezoneLabel(zone, names), description: zone };
    });
  }, [locale, settings.Timezone, t.settings.timezoneNames]);
  return <div className="settings-general" aria-busy={action.busy || undefined}>
    <div className="settings-list">
      <Select labelInteractive={false} size="sm" className="settings-row settings-row--select" align="end" label={t.settings.language} value={value.Language} disabled={action.busy} onValueChange={Language => change({ Language })} options={[{ value: 'zh', label: '简体中文' }, { value: 'en', label: 'English' }]} />
      <Select labelInteractive={false} size="sm" className="settings-row settings-row--select" align="end" label={t.settings.timezone} value={value.Timezone} disabled={action.busy} onValueChange={Timezone => change({ Timezone })} options={timezones} searchable />
      <Select labelInteractive={false} size="sm" className="settings-row settings-row--select" align="end" label={t.settings.currency} value={value.Currency} disabled={action.busy} onValueChange={Currency => change({ Currency })} options={[{ value: 'USD', label: t.settings.currencies.USD }, { value: 'CNY', label: t.settings.currencies.CNY }]} />
      <SettingsSwitch label={t.workbench.enable} help={runtime.data?.workbench_available ? t.workbench.enableHelp : t.workbench.runtimeDisabled} checked={!!runtime.data?.workbench_available && !!value.WorkbenchEnabled} disabled={action.busy || !runtime.data?.workbench_available} onChange={event => change({ WorkbenchEnabled: event.target.checked })} />
    </div>
    <ErrorNotice error={action.error || runtime.error} retry={() => { action.reset(); refresh(); }} />
  </div>;
}
