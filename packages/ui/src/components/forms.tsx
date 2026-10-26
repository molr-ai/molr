import { useId, useLayoutEffect, useRef, useState } from 'react';
import type { InputHTMLAttributes, ReactNode, TextareaHTMLAttributes } from 'react';
import { useUILocale } from '../locale';
import { UIIcon } from './icons';
import { useChoiceScroll } from '../use-choice-scroll';

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & { label: string; description?: string; error?: string };
export function Textarea({ label, description, error, id, autoComplete = 'off', className = '', rows = 4, 'aria-describedby': describedBy, ...props }: TextareaProps) {
  const generatedId = useId();
  const fieldId = id ?? generatedId;
  const hint = error || description;
  return <div className="text-field">
    <label htmlFor={fieldId}>{label}</label>
    <textarea {...props} autoComplete={autoComplete} id={fieldId} rows={rows} className={`input textarea ${className}`} aria-invalid={error ? true : props['aria-invalid']} aria-describedby={[describedBy, hint && `${fieldId}-hint`].filter(Boolean).join(' ') || undefined} />
    {hint && <p id={`${fieldId}-hint`} className={`field-hint${error ? ' field-hint--error' : ''}`}>{hint}</p>}
  </div>;
}

export type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'size'> & {
  label: string; description?: string; error?: string; hideLabel?: boolean;
  /** Controlled mixed state. Clear it in onChange/onIndeterminateChange when accepting a toggle. */
  indeterminate?: boolean;
  /** Initial mixed state for an uncontrolled checkbox; activation clears it. */
  defaultIndeterminate?: boolean;
  onIndeterminateChange?: (indeterminate: boolean) => void;
};
export function Checkbox({ label, description, error, indeterminate, defaultIndeterminate = false, onIndeterminateChange, onChange, hideLabel = false, className = '', id, 'aria-describedby': describedBy, ...props }: CheckboxProps) {
  const generatedId = useId();
  const fieldId = id ?? generatedId;
  const ref = useRef<HTMLInputElement>(null);
  const [internalMixed, setInternalMixed] = useState(defaultIndeterminate);
  const mixed = indeterminate ?? internalMixed;
  useLayoutEffect(() => { if (ref.current) ref.current.indeterminate = mixed; }, [mixed, props.checked]);
  const hint = error || description;
  return <div className={`check-field ${className}`}>
    <label className="check-field__label" htmlFor={fieldId}>
      <input {...props} id={fieldId} ref={ref} type="checkbox" onChange={event => {
        if (indeterminate === undefined) setInternalMixed(false);
        if (mixed) onIndeterminateChange?.(false);
        onChange?.(event);
        // Native activation clears indeterminate. A controlled prop stays authoritative,
        // including when the parent keeps checked unchanged and React bails out.
        event.currentTarget.indeterminate = indeterminate ?? false;
      }} aria-invalid={error ? true : props['aria-invalid']} aria-describedby={[describedBy, hint && `${fieldId}-hint`].filter(Boolean).join(' ') || undefined} />
      <span className="check-field__box" aria-hidden="true"><UIIcon name="check" className="check-field__checked-icon" /><UIIcon name="minus" className="check-field__mixed-icon" /></span>
      <span className={hideLabel ? 'ui-sr-only' : undefined}>{label}</span>
    </label>
    {hint && <p id={`${fieldId}-hint`} className={`field-hint${error ? ' field-hint--error' : ''}`}>{hint}</p>}
  </div>;
}

export type RadioOption<Value extends string = string> = { value: Value; label: string; description?: string; icon?: ReactNode; disabled?: boolean };
export type RadioGroupProps<Value extends string = string> = {
  label: string; value: Value | ''; onValueChange: (value: Value) => void; options: readonly RadioOption<Value>[];
  name?: string; disabled?: boolean; description?: string; error?: string; hideLabel?: boolean; className?: string;
};
export function RadioGroup<Value extends string>({ label, value, onValueChange, options, name, disabled, description, error, hideLabel, className = '' }: RadioGroupProps<Value>) {
  const id = useId();
  const listRef = useChoiceScroll(value);
  const hint = error || description;
  return <fieldset className={`radio-group ${className}`} disabled={disabled} aria-describedby={hint ? `${id}-hint` : undefined} aria-invalid={error ? true : undefined}>
    <legend className={hideLabel ? 'ui-sr-only' : undefined}>{label}</legend>
    <div ref={listRef} className="radio-group__options">
      {options.map(option => <label className="radio-option" key={option.value}>
        <input type="radio" name={name ?? id} value={option.value} checked={option.value === value} disabled={option.disabled} onChange={() => onValueChange(option.value)} />
        <span className="radio-option__indicator" aria-hidden="true" />
        {option.icon && <span className="radio-option__icon" aria-hidden="true">{option.icon}</span>}
        <span className="radio-option__copy"><span>{option.label}</span>{option.description && <small>{option.description}</small>}</span>
      </label>)}
    </div>
    {hint && <p id={`${id}-hint`} className={`field-hint${error ? ' field-hint--error' : ''}`}>{hint}</p>}
  </fieldset>;
}

export type SegmentedControlProps<Value extends string = string> = RadioGroupProps<Value> & { size?: 'sm' | 'md' };
export function SegmentedControl<Value extends string>({ size = 'md', ...props }: SegmentedControlProps<Value>) {
  return <RadioGroup {...props} className={`segmented-control segmented-control--${size} ${props.className ?? ''}`} />;
}

export type SearchFieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'value' | 'defaultValue' | 'onChange' | 'size'> & {
  label: string; value: string; onValueChange: (value: string) => void; hideLabel?: boolean; clearLabel?: string; trailingAction?: ReactNode;
};
export function SearchField({ label, value, onValueChange, hideLabel = false, clearLabel, trailingAction, id, autoComplete = 'off', className = '', ...props }: SearchFieldProps) {
  const generatedId = useId();
  const fieldId = id ?? generatedId;
  const ref = useRef<HTMLInputElement>(null);
  const { messages } = useUILocale();
  const canClear = !!value && !props.disabled && !props.readOnly;
  return <div className={`text-field search-field${trailingAction ? ' search-field--with-action' : ''}${canClear ? ' search-field--clearable' : ''} ${className}`}>
    <label htmlFor={fieldId} className={hideLabel ? 'ui-sr-only' : undefined}>{label}</label>
    <div className="search-field__control"><UIIcon name="search" />
      <input {...props} autoComplete={autoComplete} ref={ref} type="search" id={fieldId} value={value} className="input" onChange={event => onValueChange(event.target.value)} />
      {(canClear || trailingAction) && <div className="search-field__actions">
        {canClear && <button type="button" className="ui-icon-button search-field__clear" aria-label={clearLabel ?? messages.clear} onClick={() => { onValueChange(''); ref.current?.focus(); }}><UIIcon name="close" /></button>}
        {trailingAction}
      </div>}
    </div>
  </div>;
}
