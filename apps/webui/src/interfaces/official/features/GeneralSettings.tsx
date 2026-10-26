import { useErrorToast } from '../../../error-toasts';
import { timezoneLabel } from '../../../currency';
import { useMemo } from 'react';
import { SegmentedControl, Select } from '@molr/ui';
import type { RuntimeInfo, Settings } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { Loading } from '../components/common';
import { SettingsChoice, SettingsSwitch } from '../components/SettingsHelp';

import { useSettingsChange } from './use-settings-change';

const commonZones = ['UTC', 'Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Taipei', 'Asia/Tokyo', 'Asia/Seoul', 'Asia/Singapore', 'Asia/Dubai', 'Europe/London', 'Europe/Paris', 'Europe/Berlin', 'America/New_York', 'America/Chicago', 'America/Los_Angeles', 'Australia/Sydney', 'Pacific/Auckland'];

export function GeneralSettings() {
  const query = useQuery<Settings>('/api/settings');
  useErrorToast(query.error, query.reload);
  return <>{query.data ? <GeneralControls settings={query.data} refresh={query.reload} /> : query.loading && <Loading />}</>;
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
  useErrorToast(action.error || runtime.error, () => { action.reset(); refresh(); });
  return <div className="settings-list" aria-busy={action.busy || undefined}>
    <SettingsChoice label={t.settings.language}><SegmentedControl hideLabel label={t.settings.language} value={value.Language} disabled={action.busy} onValueChange={Language => change({ Language })} options={[{ value: 'zh', label: '简体中文' }, { value: 'en', label: 'English' }]} /></SettingsChoice>
    <Select labelInteractive={false} size="sm" className="settings-row settings-row--select" align="end" label={t.settings.timezone} value={value.Timezone} disabled={action.busy} onValueChange={Timezone => change({ Timezone })} options={timezones} searchable />
    <SettingsChoice label={t.settings.currency}><SegmentedControl hideLabel label={t.settings.currency} value={value.Currency} disabled={action.busy} onValueChange={Currency => change({ Currency })} options={[{ value: 'USD', label: t.settings.currencies.USD }, { value: 'CNY', label: t.settings.currencies.CNY }]} /></SettingsChoice>
    <SettingsSwitch label={t.workbench.enable} help={runtime.data?.workbench_available ? t.workbench.enableHelp : t.workbench.runtimeDisabled} checked={!!runtime.data?.workbench_available && !!value.WorkbenchVisible} disabled={action.busy || !runtime.data?.workbench_available} onChange={event => change({ WorkbenchVisible: event.target.checked })} />
    <SettingsSwitch label={locale === 'zh' ? '显示插件' : 'Show plugins'} help={locale === 'zh' ? '显示插件入口、插件主题和相关管理选项。关闭后保留已有插件配置和授权。' : 'Show plugin navigation, themes and management options. Hiding them preserves existing configuration and permissions.'} checked={!!value.PluginsVisible} disabled={action.busy} onChange={event => change({ PluginsVisible: event.target.checked })} />
  </div>;
}
