import { useId, useState } from 'react';
import { IconButton, TextField } from './ui';
import type { TextFieldProps } from './ui';
import { useUILocale } from '../locale';
import { Eye, EyeOff } from 'lucide-react';

export type SecretFieldProps = Omit<TextFieldProps, 'type' | 'autoComplete' | 'autoCorrect' | 'autoCapitalize' | 'spellCheck' | 'trailingAction'> & { name: string };

/** API keys and service tokens are not sign-in passwords. Use a separate name
 * and React key for each credential context so browsers cannot reuse its DOM.
 * Mask inline to avoid revealing the value while stylesheets load. Older
 * browsers without text masking retain native password concealment.
 */
export function SecretField({ name, id, style, ...props }: SecretFieldProps) {
  const generatedID = useId();
  const inputID = id ?? `${generatedID}-${name}`;
  const [visible, setVisible] = useState(false);
  const { messages } = useUILocale();
  const maskedText = typeof CSS !== 'undefined' && CSS.supports('-webkit-text-security', 'disc');
  const section = name.replace(/[^a-z0-9-]/gi, '-');
  return <TextField {...props} id={inputID} name={name}
    type={maskedText || visible ? 'text' : 'password'}
    autoComplete={maskedText ? 'off' : `section-${section} new-password`}
    autoCorrect="off" autoCapitalize="none" spellCheck={false}
    data-1p-ignore="true" data-lpignore="true" data-bwignore="true"
    style={{ ...style, ...(maskedText ? { WebkitTextSecurity: visible ? 'none' : 'disc' } : {}) }}
    trailingAction={<IconButton size="sm" disabled={props.disabled} aria-controls={inputID}
      label={visible ? messages.secretField.hideLabel : messages.secretField.showLabel}
      title={visible ? messages.secretField.hideLabel : messages.secretField.showLabel}
      onMouseDown={event => event.preventDefault()} onClick={() => setVisible(value => !value)}>
      {visible ? <EyeOff size={18} aria-hidden="true" /> : <Eye size={18} aria-hidden="true" />}
    </IconButton>} />;
}
