import { useErrorToast } from '../../../error-toasts';
import { SettingsSwitch } from '../components/SettingsHelp';
import { ThemeSelector } from '../../../ui-host/ThemeSelector';
import { hostCopy } from '../../../ui-host/copy';
import { PalettePicker, ThemeSelect, useTheme } from '@molr/ui';
import type { Settings } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { Loading } from '../components/common';
import { useSettingsChange } from './use-settings-change';

export function ThemeSettings() {
  const query = useQuery<Settings>('/api/settings');
  useErrorToast(query.error, query.reload);
  return <>{query.data ? <ThemeControls settings={query.data} /> : query.loading && <Loading />}</>;
}
function ThemeControls({ settings }: { settings: Settings }) {
  const { t, locale } = useCopy(), { theme } = useTheme();
  const { value, change, action } = useSettingsChange(settings);
  useErrorToast(action.error);
  return <div className="settings-theme" aria-busy={action.busy || undefined}>
    <div className="settings-list">
      <ThemeSelector />
      <ThemeSelect labelInteractive={false} size="sm" className="settings-row settings-row--select" label={t.settings.displayMode} compact={false} align="end" value={value.Theme} disabled={action.busy} onValueChange={Theme => change({ Theme })} />
      <SettingsSwitch label={t.settings.reduced} checked={value.ReducedMotion} disabled={action.busy} onChange={event => change({ ReducedMotion: event.target.checked })} />
    </div>
    <div className="settings-theme__palettes"><h3>{t.settings.palette}</h3><PalettePicker locale={locale} palette={value.Palette || 'forest'} theme={theme} disabled={action.busy} onChange={Palette => change({ Palette })} /></div>
    <p className="settings-theme__note">{hostCopy[locale].saved}</p>
  </div>;
}
