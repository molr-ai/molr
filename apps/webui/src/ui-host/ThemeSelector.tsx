import { Select } from '@molr/ui';
import { packageKey } from '@molr/ui-host';
import { useCopy } from '../state';
import { useUIHost } from './store';
import { hostCopy } from './copy';

export function ThemeSelector() {
  const { locale } = useCopy(), copy = hostCopy[locale], host = useUIHost();
  return <>
    <Select labelInteractive={false} className="settings-row settings-row--select" size="sm" label={copy.theme} align="end" disabled={host.busy || host.loading} value={packageKey(host.configuration.selected)} options={host.packages.map(item => ({ value: packageKey(item.manifest), label: item.manifest.id === 'molr.official' ? copy.defaultTheme : item.manifest.id === 'molr.test' ? copy.testTheme : item.manifest.name }))} onValueChange={key => {
      const selected = host.packages.find(item => packageKey(item.manifest) === key)!;
      void host.select({ id: selected.manifest.id, version: selected.manifest.version }).catch(() => {});
    }} />
    {host.error ? <p role="alert">{copy.saveFailed}</p> : null}
  </>;
}
