import { productMessages } from './i18n';
import { useState } from 'react';
import { Button, Tabs } from '@molr/ui';
import type { TabItem } from '@molr/ui';
import { MemoryPanel, UsagePanel } from './AssistantPanels';
import { FormerAssistantsPanel } from './FormerAssistantsPanel';
import type { InterfaceDemo } from './interface-demo';
import type { Locale } from './messages';

type Section = 'interface' | 'memory' | 'preferences' | 'usage' | 'connection' | 'former-assistants' | 'data';
export function SettingsPanel({ demo, locale, active = true, initialSection = 'preferences' }: { demo: InterfaceDemo; locale: Locale; active?: boolean; initialSection?: 'preferences' | 'connection' }) {
  const t = productMessages[locale];
  const [section, setSection] = useState<Section>(initialSection);
  const items: TabItem<Section>[] = [
    { value: 'interface', label: t.settingsPanel.interface, content: <><p className="ip-lead">{t.settingsPanel.chooseWhatAppearsInConversationsSharedAcrossAssistantsAnd}</p><div className="interface-options">{([
        ['showMetrics', t.settingsPanel.messageInformationIcon, t.settingsPanel.clickTheIconToInspectInputOutputFirstToken],
        ['showAvatar', t.settingsPanel.messageAvatars, t.settingsPanel.showYourAvatarAndTheAssistantSAvatarBeside],
        ['showSender', t.settingsPanel.senderNames, t.settingsPanel.showYourNameAndTheAssistantSName],
        ['showHints', t.settingsPanel.helperText, t.settingsPanel.showConversationHelpAndInputShortcuts],
        ['showFeedback', t.settingsPanel.likeAndDislike, t.settingsPanel.showFeedbackControlsUnderAssistantMessages],
      ] as const).map(([key, label, description]) => <label key={key}><span><strong>{label}</strong><small>{description}</small></span><input type="checkbox" role="switch" checked={demo.interfacePreferences[key]} onChange={event => demo.setInterfacePreference(key, event.target.checked)} /></label>)}</div></> },
    { value: 'memory', label: t.settingsPanel.memory, content: <MemoryPanel kind="memory" locale={locale} demo={demo} /> },
    { value: 'preferences', label: t.settingsPanel.preferences, content: <MemoryPanel kind="preference" locale={locale} demo={demo} /> },
    { value: 'usage', label: t.settingsPanel.usage, content: <><p className="ip-lead">{t.settingsPanel.setBudgetsAndCallLimitsForNewTasksReview}</p><p className="ip-muted">{t.settingsPanel.currentAssistant}{demo.activeAssistant.name}</p><UsagePanel locale={locale} demo={demo} section="usage" /></> },
    { value: 'connection', label: t.settingsPanel.connections, content: <><UsagePanel locale={locale} demo={demo} section="connection" /><div className="ip-work-card"><div className="ip-section-title"><h4>{t.settingsPanel.localConnection}</h4><span className="ip-status">{demo.connected ? t.settingsPanel.connected : t.settingsPanel.disconnected}</span></div><p>{t.settingsPanel.manageThisInterfaceSServiceConnectionDisconnectingDoesNot}</p><Button size="sm" variant="secondary" onClick={() => demo.setConnected(!demo.connected)}>{demo.connected ? t.settingsPanel.simulateDisconnect : t.settingsPanel.reconnect}</Button></div></> },
    { value: 'former-assistants', label: t.settingsPanel.formerAssistants, content: <FormerAssistantsPanel locale={locale} active={active && section === 'former-assistants'} /> },
    { value: 'data', label: t.settingsPanel.data, content: <UsagePanel locale={locale} demo={demo} section="data" /> },
  ];
  return <Tabs className="settings-tabs" label={t.settingsPanel.settingsSections} items={items} value={section} onValueChange={setSection} />;
}
