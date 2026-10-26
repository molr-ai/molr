import { Select } from './Select';
import { SegmentedControl } from './forms';
import type { SelectOption } from './Select';
import type { ThemePreference } from '../use-theme';
import { useUILocale } from '../locale';
import type { UILocale } from '../locale';

function ThemeIcon({ theme }: { theme: ThemePreference }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {theme === 'system' ? <><rect x="2" y="3" width="20" height="14" rx="2" /><path d="M8 21h8M12 17v4" /></> : theme === 'light' ? <><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.65 17.65l1.42 1.42M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.65 6.35l1.42-1.42" /></> : <path d="M20.99 12.91A9 9 0 0 1 11.09 3a9 9 0 1 0 9.9 9.91Z" />}
    </svg>
  );
}

const messages = {
  zh: {
    label: '外观模式',
    options: [
      { value: 'system', label: '跟随系统', description: '与设备外观保持一致', icon: <ThemeIcon theme="system" /> },
      { value: 'light', label: '浅色模式', description: '明亮底色，清晰文字', icon: <ThemeIcon theme="light" /> },
      { value: 'dark', label: '暗色模式', description: '深邃底色，柔和文字', icon: <ThemeIcon theme="dark" /> },
    ],
  },
  en: {
    label: 'Appearance',
    options: [
      { value: 'system', label: 'System', description: 'Match your device appearance', icon: <ThemeIcon theme="system" /> },
      { value: 'light', label: 'Light', description: 'Light surfaces with clear text', icon: <ThemeIcon theme="light" /> },
      { value: 'dark', label: 'Dark', description: 'Dark surfaces with soft text', icon: <ThemeIcon theme="dark" /> },
    ],
  },
} as const satisfies Record<string, { label: string; options: readonly SelectOption<ThemePreference>[] }>;

type ThemeSelectProps = {
  value: ThemePreference;
  onValueChange: (value: ThemePreference) => void;
  className?: string;
  locale?: UILocale;
  label?: string;
  compact?: boolean;
  variant?: 'select' | 'segmented';
  labelInteractive?: boolean;
  size?: 'sm' | 'md';
  disabled?: boolean;
  align?: 'start' | 'end';
};

export function ThemeSelect({ value, onValueChange, className = '', locale, label, compact = true, variant = 'select', size, disabled, align, labelInteractive }: ThemeSelectProps) {
  const inherited = useUILocale();
  const copy = messages[locale ?? inherited.locale];
  if (variant === 'segmented') return <SegmentedControl label={label ?? copy.label} hideLabel={compact} className={className} disabled={disabled} options={copy.options.map(({ value, label }) => ({ value, label }))} value={value} onValueChange={onValueChange} />;
  return <Select label={label ?? copy.label} labelInteractive={labelInteractive} hideLabel={compact} size={size ?? (compact ? 'sm' : 'md')} align={align ?? (compact ? 'end' : 'start')} disabled={disabled} className={`${compact ? 'theme-select' : ''} ${className}`} options={copy.options} value={value} onValueChange={onValueChange} />;
}
