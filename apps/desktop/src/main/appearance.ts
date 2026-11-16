import { readFileSync, writeFileSync, renameSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import type { AppAppearance } from '@molr/ui-host/app';

const palettes = ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome'];
function valid(value: unknown): value is AppAppearance {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const appearance = value as AppAppearance;
  return Object.keys(value).length === 3
    && Object.keys(value).every(key => key === 'theme' || key === 'palette' || key === 'fontSize')
    && ['system', 'light', 'dark'].includes(appearance.theme)
    && palettes.includes(appearance.palette)
    && ['small', 'standard', 'large'].includes(appearance.fontSize);
}
export class AppearanceStore {
  value: AppAppearance = { theme: 'system', palette: 'forest', fontSize: 'standard' };
  constructor(private file: string) {
    try {
      const raw = readFileSync(file, 'utf8');
      if (raw.length > 1024) return;
      const saved: unknown = JSON.parse(raw);
      if (saved && typeof saved === 'object' && !Array.isArray(saved) && Object.keys(saved).length === 2 && 'theme' in saved && 'palette' in saved) {
        Object.assign(saved, { fontSize: 'standard' });
        if (valid(saved)) writeFileSync(file, JSON.stringify(saved), { mode: 0o600 });
      }
      if (valid(saved)) this.value = { ...saved };
    } catch { /* Missing or damaged cosmetic preferences use the defaults. */ }
  }
  set(value: unknown) {
    if (!valid(value)) throw new Error('无效的桌面外观设置。');
    if (value.theme === this.value.theme && value.palette === this.value.palette && value.fontSize === this.value.fontSize) return false;
    const next = { ...value }, temporary = `${this.file}.${randomUUID()}.tmp`;
    writeFileSync(temporary, JSON.stringify(next), { mode: 0o600, flag: 'wx' });
    renameSync(temporary, this.file);
    this.value = next;
    return true;
  }
}
