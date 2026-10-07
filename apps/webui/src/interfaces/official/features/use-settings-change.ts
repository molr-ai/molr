import { useRef, useState } from 'react';
import { useTheme } from '@molr/ui';
import type { Settings } from '../../../api/types';
import { api } from '../../../api/client';
import { useAppearance, useCopy } from '../../../state';
import { useAction } from '../components/common';

type SettingsPatch = Partial<Pick<Settings, 'Language' | 'Timezone' | 'Currency' | 'Theme' | 'Palette' | 'ReducedMotion' | 'WorkbenchEnabled'>>;
/** Merge only edited fields into the latest version; failed saves restore the last confirmed value. */
export function useSettingsChange(settings: Settings) {
  const { setLocale } = useCopy(), { changePreference } = useTheme(), { changePalette } = useAppearance(), action = useAction();
  const [saved, setSaved] = useState(settings), [pending, setPending] = useState<SettingsPatch | null>(null);
  const writing = useRef(false);
  const current = saved.Version > settings.Version ? saved : settings;
  function change(patch: SettingsPatch) {
    if (writing.current) return;
    writing.current = true; setPending(patch);
    void action.run(async () => {
      const latest = await api.get<Settings>('/api/settings');
      const updated = await api.mutate<Settings>('/api/settings', { Settings: { ...latest, ...patch } }, latest.Version);
      setSaved(updated);
      if (patch.Language && (updated.Language === 'zh' || updated.Language === 'en')) setLocale(updated.Language);
      if (patch.Theme) changePreference(updated.Theme);
      if (patch.Palette) changePalette(updated.Palette);
      if (patch.ReducedMotion !== undefined) document.documentElement.dataset.reducedMotion = String(updated.ReducedMotion);
    }, undefined, false).finally(() => { writing.current = false; setPending(null); });
  }
  return { value: { ...current, ...pending }, change, action };
}
