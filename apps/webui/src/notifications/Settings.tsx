import { useState } from 'react';
import { Button } from '@molr/ui';
import { useCopy } from '../state';
import { SettingsSwitch } from '../interfaces/official/components/SettingsHelp';
import { useNotifications } from './provider';
import { notificationCopy } from './copy';

export function NotificationSettings() {
  const { locale } = useCopy(), copy = notificationCopy[locale], notifications = useNotifications();
  const [busy, setBusy] = useState(false), [result, setResult] = useState('');
  const systemAvailable = !['unsupported', 'insecure'].includes(notifications.capability);
  return <div className="settings-general notification-settings">
    <p className="muted">{copy.help}</p>
    <div className="settings-list">
      <SettingsSwitch label={copy.enabled} help={copy.enabledHelp} checked={notifications.preferences.enabled} onChange={event => notifications.change({ enabled: event.target.checked })} />
      <SettingsSwitch label={copy.preview} help={copy.previewHelp} checked={notifications.preferences.preview} onChange={event => notifications.change({ preview: event.target.checked })} />
      {notifications.soundState !== 'unsupported' && <SettingsSwitch label={copy.sound} help={copy.soundHelp} checked={notifications.preferences.sound} onChange={event => notifications.change({ sound: event.target.checked })} />}
      {systemAvailable && <SettingsSwitch label={copy.system} help={copy.systemHelp} checked={notifications.preferences.system} onChange={event => notifications.change({ system: event.target.checked })} />}
    </div>
    {systemAvailable && notifications.preferences.system && <p role="status">{copy.status[notifications.capability]}</p>}
    {notifications.preferences.sound && notifications.soundState === 'blocked' && <p role="status">{copy.soundBlocked}</p>}
    <div className="form-actions">
      {systemAvailable && notifications.preferences.system && notifications.capability === 'default' && <Button disabled={busy} onClick={() => { setBusy(true); void notifications.requestPermission().finally(() => setBusy(false)); }}>{copy.allow}</Button>}
      <Button variant="secondary" disabled={busy} onClick={() => { setBusy(true); void notifications.test().then(system => setResult(system ? copy.sent : copy.fallback)).finally(() => setBusy(false)); }}>{copy.test}</Button>
      {notifications.soundState !== 'unsupported' && <Button variant="secondary" disabled={busy} onClick={() => { setBusy(true); void notifications.testSound().then(played => setResult(played ? copy.soundPlayed : copy.soundBlocked)).finally(() => setBusy(false)); }}>{copy.testSound}</Button>}
    </div>
    {result && <p role="status">{result}</p>}
    <p className="muted">{copy.local}</p>
  </div>;
}
