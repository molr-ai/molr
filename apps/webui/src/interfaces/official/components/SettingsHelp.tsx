import { useId } from 'react';
import { Switch, Tooltip } from '@molr/ui';
import type { SwitchProps } from '@molr/ui';
import { Info } from 'lucide-react';
import { useCopy } from '../../../state';

export function SettingsHelp({ label, content }: { label: string; content: string }) {
  const { t } = useCopy();
  return <Tooltip content={content} width={280} openOnClick>
    <button type="button" className="ui-icon-button settings-help" aria-label={t.information(label)}><Info size={16} aria-hidden="true" /></button>
  </Tooltip>;
}

export function SettingsSwitch({ label, help, id, ...props }: Omit<SwitchProps, 'className'> & { help?: string }) {
  const generatedID = useId(), fieldID = id ?? generatedID;
  return <div className="settings-row settings-row--with-help">
    <div className="settings-label"><span>{label}</span>{help && <SettingsHelp label={label} content={help} />}</div>
    <Switch size="lg" {...props} id={fieldID} label="" aria-label={label} className="settings-switch-control" />
  </div>;
}
