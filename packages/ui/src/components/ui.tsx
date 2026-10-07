import { useId } from 'react';
import type { ComponentPropsWithRef, HTMLAttributes, ReactNode } from 'react';

export type ButtonProps = ComponentPropsWithRef<'button'> & {
  variant?: 'primary' | 'accent' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md' | 'lg';
  loading?: boolean;
};

export function Button({ variant = 'primary', size = 'md', className = '', type = 'button', loading = false, children, ...props }: ButtonProps) {
  return <button type={type} className={`button button--${variant} button--${size} ${className}`} {...props} disabled={props.disabled || loading} aria-busy={loading || undefined}>{loading && <span className="ui-spinner" aria-hidden="true" />}{children}</button>;
}

export function IconButton({ label, className = '', ...props }: ButtonProps & { label: string }) {
  return <Button variant="ghost" {...props} aria-label={label} className={`ui-icon-button ${className}`} />;
}

export type TextFieldProps = Omit<ComponentPropsWithRef<'input'>, 'size'> & {
  label: string;
  description?: string;
  error?: string;
  trailingAction?: ReactNode;
};

export function TextField({ label, description, error, trailingAction, id, autoComplete = 'off', className = '', 'aria-describedby': describedBy, ...props }: TextFieldProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;
  const hint = error || description;
  const hintId = `${inputId}-hint`;

  const input = <input
    {...props}
    autoComplete={autoComplete}
    id={inputId}
    className="input"
    aria-invalid={error ? true : props['aria-invalid']}
    aria-describedby={[describedBy, hint ? hintId : undefined].filter(Boolean).join(' ') || undefined}
  />;

  return (
    <div className={`text-field ${className}`}>
      <label htmlFor={inputId}>{label}</label>
      {trailingAction ? <div className="text-field__control">{input}<div className="text-field__action">{trailingAction}</div></div> : input}
      {hint && <p id={hintId} className={`field-hint ${error ? 'field-hint--error' : ''}`}>{hint}</p>}
    </div>
  );
}

export type BadgeProps = HTMLAttributes<HTMLSpanElement> & {
  tone?: 'neutral' | 'positive' | 'warning' | 'danger';
  size?: 'sm' | 'md';
  dot?: boolean;
};

export function Badge({ tone = 'neutral', size = 'md', dot = true, className = '', children, ...props }: BadgeProps) {
  return (
    <span className={`badge badge--${tone} badge--${size} ${className}`} {...props}>
      {dot && <span className="badge__dot" aria-hidden="true" />}
      {children}
    </span>
  );
}

export type SwitchProps = Omit<ComponentPropsWithRef<'input'>, 'type' | 'role' | 'size'> & {
  size?: 'sm' | 'md' | 'lg';
  label: string;
};

export function Switch({ label, size = 'md', className = '', ...props }: SwitchProps) {
  return (
    <label className={`switch-field switch-field--${size} ${className}`}>
      <input {...props} type="checkbox" role="switch" />
      <span className="switch-control" aria-hidden="true"><span /></span>
      <span>{label}</span>
    </label>
  );
}

export function Card({ className = '', children, ...props }: HTMLAttributes<HTMLDivElement> & { children: ReactNode }) {
  return <div className={`card ${className}`} {...props}>{children}</div>;
}
