import { productMessages } from './i18n';
import { Globe, Monitor, Folder, Wifi, BatteryFull } from 'lucide-react';
import { Button } from '@molr/ui';
import type { Locale } from './messages';
import { demoResources } from './interface-demo';

type PlatformProps = { locale: Locale };
type DirectoryPickerProps = PlatformProps & { onSelect: (id: string) => void; onCancel: () => void };
type NotificationProps = PlatformProps & { onDismiss: () => void };
export type InterfacePlatform = {
  id: 'web' | 'desktop' | 'mobile';
  Chrome: (props: PlatformProps & { page: string }) => React.ReactNode;
  Notification: (props: NotificationProps) => React.ReactNode;
  DirectoryPicker: (props: DirectoryPickerProps) => React.ReactNode;
};

function DirectoryPicker({ locale, onSelect, onCancel, host }: DirectoryPickerProps & { host: string }) {

  return <div className="ip-platform-picker" role="group" aria-label={productMessages[locale].interfacePlatform.chooseDirectory}>
    <div className="ip-section-title"><h4>{host}</h4><Button size="sm" variant="ghost" onClick={onCancel}>{productMessages[locale].common.cancel}</Button></div>
    <p className="ip-muted">{productMessages[locale].interfacePlatform.directoryPickerPreviewSampleResourcesOnlyNoLocalFiles}</p>
    {demoResources.filter(item => item.category === 'Storage' && item.id !== 'archive').map(item => <button className="ip-row" type="button" key={item.id} onClick={() => onSelect(item.id)}><Folder size={16} /><span className="ip-row__body"><strong>{item[locale]}</strong><small>{item.path}</small></span></button>)}
  </div>;
}
function NotificationPreview({ locale, onDismiss, host }: NotificationProps & { host: string }) {

  return <aside className="ip-system-notification" role="status" aria-label={productMessages[locale].interfacePlatform.systemNotificationPreview}>
    <small>{host} · {productMessages[locale].interfacePlatform.simulatedNotification}</small><strong>Molr</strong><p>{productMessages[locale].interfacePlatform.travelDigestCompleted3SourcesVerified}</p><Button size="sm" variant="ghost" onClick={onDismiss}>{productMessages[locale].interfacePlatform.dismissNotification}</Button>
  </aside>;
}
const webPlatform: InterfacePlatform = {
  id: 'web',
  Chrome: ({ page }) => <div className="ip-chrome"><Globe size={14} /><span>molr.local / {page}</span><span>Web</span></div>,
  Notification: props => <NotificationPreview {...props} host={productMessages[props.locale].interfacePlatform.browserNotificationMolrLocal} />,
  DirectoryPicker: props => <DirectoryPicker {...props} host={productMessages[props.locale].interfacePlatform.browserDirectoryPicker} />,
};
const desktopPlatform: InterfacePlatform = {
  id: 'desktop',
  Chrome: ({ locale }) => <div className="ip-chrome"><span className="ip-dots" aria-hidden="true">● ● ●</span><span>Molr — {productMessages[locale].interfacePlatform.personalSpace}</span><Monitor size={14} /></div>,
  Notification: props => <NotificationPreview {...props} host={productMessages[props.locale].interfacePlatform.desktopNotificationCenterMolr} />,
  DirectoryPicker: props => <DirectoryPicker {...props} host={productMessages[props.locale].interfacePlatform.systemDirectoryPicker} />,
};
const mobilePlatform: InterfacePlatform = {
  id: 'mobile',
  Chrome: ({ locale }) => <div className="ip-mobile-status" aria-label={productMessages[locale].interfacePlatform.phoneStatusBarPreview}><span>9:41</span><span className="ip-mobile-island" aria-hidden="true" /><span aria-hidden="true"><Wifi size={14} /><BatteryFull size={19} /></span></div>,
  Notification: props => <NotificationPreview {...props} host={productMessages[props.locale].interfacePlatform.mobilePushMolr} />,
  DirectoryPicker: props => <DirectoryPicker {...props} host={productMessages[props.locale].interfacePlatform.chooseAConnectedDirectory} />,
};
// Design-preview adapters. Production browser/native integrations implement this boundary.
export const interfacePlatforms = { web: webPlatform, desktop: desktopPlatform, mobile: mobilePlatform };
