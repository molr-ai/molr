import { readFileSync, writeFileSync, renameSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import type { AppAppearance } from '@molr/ui-host/app';

const palettes = ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome'];
function valid(value: unknown): value is AppAppearance {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const appearance = value as AppAppearance;
  return Object.keys(value).length === 2
    && Object.keys(value).every(key => key === 'theme' || key === 'palette')
    && ['system', 'light', 'dark'].includes(appearance.theme)
    && palettes.includes(appearance.palette);
}
export class AppearanceStore {
  value: AppAppearance = { theme: 'system', palette: 'forest' };
  constructor(private file: string) {
    try {
      const raw = readFileSync(file, 'utf8');
      if (raw.length > 1024) return;
      const saved: unknown = JSON.parse(raw);
      if (valid(saved)) this.value = { ...saved };
    } catch { /* Missing or damaged cosmetic preferences use the defaults. */ }
  }
  set(value: unknown) {
    if (!valid(value)) throw new Error('无效的桌面外观设置。');
    if (value.theme === this.value.theme && value.palette === this.value.palette) return false;
    const next = { ...value }, temporary = `${this.file}.${randomUUID()}.tmp`;
    writeFileSync(temporary, JSON.stringify(next), { mode: 0o600, flag: 'wx' });
    renameSync(temporary, this.file);
    this.value = next;
    return true;
  }
}
