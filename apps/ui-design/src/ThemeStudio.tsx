import { useEffect, useState } from 'react';
import { Check, Copy } from 'lucide-react';
import { PalettePicker } from '@molr/ui';
import type { Theme } from '@molr/ui';
import type { Locale } from './messages';
import { designThemes } from './design-themes';
import type { DesignPalette } from './design-themes';

type Props = { locale: Locale; palette: DesignPalette; onChange: (palette: DesignPalette) => void };
export { PaletteSelect, ThemeSelect as DesignAppearanceSelect } from '@molr/ui';

const roles = [
  { token: 'primary', en: 'Primary', zh: '主色', description: { zh: '主要操作', en: 'Key actions' } },
  { token: 'accent', en: 'Accent', zh: '强调', description: { zh: '重点与高亮', en: 'Highlights' } },
  { token: 'background', en: 'Canvas', zh: '画布', description: { zh: '页面底色', en: 'Page background' } },
  { token: 'surface', en: 'Surface', zh: '表面', description: { zh: '卡片与容器', en: 'Cards & containers' } },
  { token: 'text-muted', en: 'Muted', zh: '辅助', description: { zh: '次级文字', en: 'Supporting text' } },
  { token: 'border', en: 'Border', zh: '边界', description: { zh: '分隔与层次', en: 'Separation & layers' } },
] as const;
function isDark(hex: string) {
  const channels = hex.replace('#', '').match(/.{2}/g)?.map(value => parseInt(value, 16) / 255) ?? [];
  const [r, g, b] = channels.map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
  return .2126 * r + .7152 * g + .0722 * b < .179;
}

export function ThemeStudio({ locale, palette, onChange, theme, copied, onCopy }: Props & { theme: Theme; copied: string | null; onCopy: (color: string) => void }) {
  const [colors, setColors] = useState<string[]>([]);
  useEffect(() => {
    const style = getComputedStyle(document.documentElement);
    setColors(roles.map(role => style.getPropertyValue(`--color-${role.token}`).trim().toUpperCase()));
  }, [palette, theme]);
  const current = designThemes.find(item => item.id === palette)!;
  return <div className="theme-studio">
    <div className="theme-studio__intro"><span>{locale === 'zh' ? '选择一种心情，让整个界面随之改变。' : 'A different mood for every workspace.'}</span><span>{locale === 'zh' ? '5 套配色 · 明暗双模式' : '5 palettes · Light & dark'}</span></div>
    <PalettePicker locale={locale} palette={palette} onChange={onChange} theme={theme} />
    <div className="theme-studio__current" aria-live="polite"><strong>{current[locale]} <span>/ {theme === 'dark' ? locale === 'zh' ? '深色' : 'Dark' : locale === 'zh' ? '浅色' : 'Light'}</span></strong><span>{locale === 'zh' ? '实时应用 · 自动记住你的选择' : 'Applied live · Your choice is saved'}</span></div>
    <div className="palette">
      {roles.map((role, index) => <button key={role.token} className="color-card" type="button" onClick={() => colors[index] && onCopy(colors[index])} aria-label={`${locale === 'zh' ? '复制' : 'Copy'} ${role[locale]} ${colors[index] ?? ''}`}>
        <span className="color-card__swatch" style={{ background: `var(--color-${role.token})`, color: colors[index] && isDark(colors[index]) ? 'var(--color-white)' : 'var(--color-contrast-dark)' }}><span>{role.en}</span>{copied === colors[index] ? <Check size={15} /> : <Copy size={14} />}<span className="color-card__hex">{colors[index]}</span></span>
        <span className="color-card__name">{role[locale]}</span><span className="color-card__role">{role.description[locale]}</span>
      </button>)}
    </div>
  </div>;
}
