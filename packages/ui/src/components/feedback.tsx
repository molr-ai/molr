import type { CSSProperties, HTMLAttributes, ReactNode } from 'react';
import { useUILocale } from '../locale';
import { UIIcon } from './icons';

export type Tone = 'neutral' | 'positive' | 'warning' | 'danger';
export type AlertProps = { title: string; children?: ReactNode; tone?: Tone; action?: ReactNode; onDismiss?: () => void; dismissLabel?: string; className?: string };
export function Alert({ title, children, tone = 'neutral', action, onDismiss, dismissLabel, className = '' }: AlertProps) {
  const { messages } = useUILocale();
  return <div className={`ui-alert ui-alert--${tone} ${className}`} role={tone === 'danger' ? 'alert' : 'status'}>
    <UIIcon name={tone === 'positive' ? 'check' : tone === 'warning' || tone === 'danger' ? 'warning' : 'info'} />
    <div className="ui-alert__content"><strong>{title}</strong>{children && <div>{children}</div>}{action && <div className="ui-alert__action">{action}</div>}</div>
    {onDismiss && <button type="button" className="ui-icon-button" aria-label={dismissLabel ?? messages.close} onClick={onDismiss}><UIIcon name="close" /></button>}
  </div>;
}

export type EmptyStateProps = { title: string; description?: string; illustration?: ReactNode; action?: ReactNode; className?: string };
export function EmptyState({ title, description, illustration, action, className = '' }: EmptyStateProps) {
  return <div className={`ui-empty ${className}`}>
    {illustration && <div className="ui-empty__art" aria-hidden="true">{illustration}</div>}
    <h3>{title}</h3>{description && <p>{description}</p>}{action && <div className="ui-empty__action">{action}</div>}
  </div>;
}

export type SkeletonProps = HTMLAttributes<HTMLSpanElement> & { width?: CSSProperties['width']; height?: CSSProperties['height']; shape?: 'line' | 'circle' | 'block' };
export function Skeleton({ width = '100%', height, shape = 'line', className = '', style, ...props }: SkeletonProps) {
  return <span {...props} aria-hidden="true" className={`ui-skeleton ui-skeleton--${shape} ${className}`} style={{ width, height, ...style }} />;
}

export type ProgressProps = { label: string; value?: number; max?: number; valueText?: string; showValue?: boolean; className?: string };
export function Progress({ label, value, max = 100, valueText, showValue = true, className = '' }: ProgressProps) {
  const { locale, messages } = useUILocale();
  const maximum = Number.isFinite(max) && max > 0 ? max : 100;
  const current = value !== undefined && Number.isFinite(value) ? Math.max(0, Math.min(maximum, value)) : undefined;
  const percentage = current === undefined ? undefined : current / maximum;
  const text = valueText ?? (percentage === undefined ? messages.loading : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 }).format(percentage));
  return <div className={`ui-progress ${className}`}>
    <div className="ui-progress__label"><span>{label}</span>{showValue && <span>{text}</span>}</div>
    <div className={`ui-progress__track${current === undefined ? ' ui-progress__track--indeterminate' : ''}`} role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={maximum} aria-valuenow={current} aria-valuetext={text}>
      <span style={{ width: percentage === undefined ? '36%' : `${percentage * 100}%` }} />
    </div>
  </div>;
}
