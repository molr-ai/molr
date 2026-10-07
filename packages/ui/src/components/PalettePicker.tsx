import { Select } from './Select';
import type { UILocale } from '../locale';
import type { Theme } from '../use-theme';
import { palettes } from '../palettes';
import type { Palette } from '../palettes';

type Props = { locale: UILocale; palette: Palette; onChange: (palette: Palette) => void; disabled?: boolean };
export function PaletteSelect({ locale, palette, onChange, disabled }: Props) {
  return <Select<Palette> label={locale === 'zh' ? '主题配色' : 'Theme colors'} hideLabel size="sm" align="end" disabled={disabled} className="design-palette-select" value={palette} onValueChange={onChange} options={palettes.map(item => ({ value: item.id, label: item[locale], icon: <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true"><path d="M12 3a9 9 0 1 0 0 18h1a2 2 0 0 0 0-4h-1a1.5 1.5 0 0 1 0-3h3a6 6 0 0 0 0-12Z" /><path d="M7 8h.01M12 6h.01M6 13h.01M17 8h.01" /></svg>, description: item.description[locale] }))} />;
}

export function PalettePicker({ locale, palette, onChange, theme, disabled }: Props & { theme: Theme }) {
  return (
    <div className="theme-options" role="group" aria-label={locale === 'zh' ? '主题配色' : 'Color palettes'}>
      {palettes.map((item, index) => <button key={item.id} type="button" className="theme-option" aria-label={item[locale]} aria-pressed={palette === item.id} disabled={disabled} onClick={() => onChange(item.id)}>
        <span className="theme-option__preview" data-palette={item.id} data-theme={theme} aria-hidden="true">
          <span className="theme-mini-sidebar"><i /><i /><i /></span>
          <span className="theme-mini-content"><span className="theme-mini-title" /><span className="theme-mini-line" /><span className="theme-mini-card"><span /><span /></span><span className="theme-mini-action" /></span>
        </span>
        <span className="theme-option__heading"><span><small>0{index + 1}</small>{item[locale]}</span><span className="theme-option__check">{palette === item.id && <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>}</span></span>
        <span className="theme-option__description">{item.description[locale]}</span>
      </button>)}
    </div>
  );
}
