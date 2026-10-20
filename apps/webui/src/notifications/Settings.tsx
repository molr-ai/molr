import { useRef, useState } from 'react';
import { Button } from '@molr/ui';
import { BellRing, Volume2 } from 'lucide-react';
import { useCopy } from '../state';
import { SettingsSwitch } from '../interfaces/official/components/SettingsHelp';
import { useNotifications } from './provider';
import { notificationCopy } from './copy';
import './settings.css';

export function NotificationSettings() {
  const { locale } = useCopy(), copy = notificationCopy[locale], notifications = useNotifications();
  const desktop = !!window.molrApp;
  const testRevision = useRef(0);
  const [testing, setTesting] = useState(false), [playing, setPlaying] = useState(false);
  const [testResult, setTestResult] = useState<'sent' | 'fallback' | 'failed'>(), [soundResult, setSoundResult] = useState<boolean>();
  const systemAvailable = !['unsupported', 'insecure'].includes(notifications.capability);
  const status = !notifications.preferences.system ? copy.systemOff
    : notifications.deliveryFailed ? (desktop ? copy.desktopFailed : copy.failed)
    : copy.status[notifications.capability];
  const result = testResult === 'sent' ? (desktop ? copy.desktopSent : copy.sent)
    : testResult === 'failed' ? (desktop ? copy.desktopFailed : copy.failed) : testResult ? copy.fallback : undefined;
  return <div className="settings-general notification-settings">
    <p className="settings-description">{desktop ? copy.desktopHelp : copy.help}</p>
    <div className="settings-list">
      <SettingsSwitch label={copy.enabled} help={copy.enabledHelp} checked={notifications.preferences.enabled} onChange={event => notifications.change({ enabled: event.target.checked })} />
      <SettingsSwitch label={copy.preview} help={copy.previewHelp} checked={notifications.preferences.preview} onChange={event => notifications.change({ preview: event.target.checked })} />
      <section className="settings-row notification-setting" aria-label={copy.system}>
        {systemAvailable ? <SettingsSwitch label={copy.system} help={copy.systemHelp} checked={notifications.preferences.system} onChange={event => { testRevision.current++; setTestResult(undefined); notifications.change({ system: event.target.checked }); }} /> : <div className="notification-setting-label">{copy.test}</div>}
        <div className="notification-setting-detail">
          <div className="notification-setting-copy" role="status" aria-live="polite">
            {systemAvailable && <p>{notifications.deliveryFailed ? status : result ?? status}</p>}
            {!systemAvailable && <p>{result ?? copy.inAppTestHelp}</p>}
            {desktop && notifications.preferences.system && <p>{copy.systemSettingsHelp}</p>}
          </div>
          <Button variant="secondary" size="sm" disabled={testing} onClick={() => { const revision = ++testRevision.current; setTesting(true); void notifications.test().then(result => { if (revision === testRevision.current) setTestResult(result); }).finally(() => setTesting(false)); }}><BellRing size={14} aria-hidden="true" />{copy.test}</Button>
        </div>
      </section>
      {notifications.soundState !== 'unsupported' && <section className="settings-row notification-setting" aria-label={copy.sound}>
        <SettingsSwitch label={copy.sound} help={copy.soundHelp} checked={notifications.preferences.sound} onChange={event => { setSoundResult(undefined); notifications.change({ sound: event.target.checked }); }} />
        <div className="notification-setting-detail">
          <div className="notification-setting-copy" role="status" aria-live="polite"><p>{soundResult === true ? copy.soundPlayed : soundResult === false || notifications.preferences.sound && notifications.soundState === 'blocked' ? (desktop ? copy.desktopSoundBlocked : copy.soundBlocked) : copy.soundTestHelp}</p></div>
          <Button variant="secondary" size="sm" disabled={playing} onClick={() => { setPlaying(true); void notifications.testSound().then(setSoundResult).finally(() => setPlaying(false)); }}><Volume2 size={14} aria-hidden="true" />{copy.testSound}</Button>
        </div>
      </section>}
    </div>
    <p className="settings-description notification-settings-local">{desktop ? copy.desktopLocal : copy.local}</p>
  </div>;
}
