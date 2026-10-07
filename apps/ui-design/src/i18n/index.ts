import type { Locale } from '../messages';
import { appMessages } from './app';
import { commonMessages } from './common';
import { assistantActionDialogMessages } from './assistantActionDialog';
import { assistantChatMessages } from './assistantChat';
import { assistantPanelsMessages } from './assistantPanels';
import { assistantSidebarMessages } from './assistantSidebar';
import { assistantWorkspaceMessages } from './assistantWorkspace';
import { chatMessageViewMessages } from './chatMessageView';
import { chatShowcaseMessages } from './chatShowcase';
import { formerAssistantsPanelMessages } from './formerAssistantsPanel';
import { interfaceShowcaseMessages } from './interfaceShowcase';
import { interfaceTerminalMessages } from './interfaceTerminal';
import { interfaceWorkspaceMessages } from './interfaceWorkspace';
import { messageComposerMessages } from './messageComposer';
import { scheduleMonthMessages } from './scheduleMonth';
import { schedulePanelMessages } from './schedulePanel';
import { settingsPanelMessages } from './settingsPanel';
import { taskDrawerMessages } from './taskDrawer';
import { todoPanelMessages } from './todoPanel';
import { workspaceSearchPanelMessages } from './workspaceSearchPanel';
import { interfaceDemoMessages } from './interfaceDemo';
import { interfacePlatformMessages } from './interfacePlatform';
import { logoConnectionShowcaseMessages } from './logoConnectionShowcase';

const zh = {
  app: appMessages.zh,
  common: commonMessages.zh,
  assistantActionDialog: assistantActionDialogMessages.zh,
  assistantChat: assistantChatMessages.zh,
  assistantPanels: assistantPanelsMessages.zh,
  assistantSidebar: assistantSidebarMessages.zh,
  assistantWorkspace: assistantWorkspaceMessages.zh,
  chatMessageView: chatMessageViewMessages.zh,
  chatShowcase: chatShowcaseMessages.zh,
  formerAssistantsPanel: formerAssistantsPanelMessages.zh,
  interfaceShowcase: interfaceShowcaseMessages.zh,
  interfaceTerminal: interfaceTerminalMessages.zh,
  interfaceWorkspace: interfaceWorkspaceMessages.zh,
  messageComposer: messageComposerMessages.zh,
  scheduleMonth: scheduleMonthMessages.zh,
  schedulePanel: schedulePanelMessages.zh,
  settingsPanel: settingsPanelMessages.zh,
  taskDrawer: taskDrawerMessages.zh,
  todoPanel: todoPanelMessages.zh,
  workspaceSearchPanel: workspaceSearchPanelMessages.zh,
  interfaceDemo: interfaceDemoMessages.zh,
  interfacePlatform: interfacePlatformMessages.zh,
  logoConnectionShowcase: logoConnectionShowcaseMessages.zh,
};

export type ProductMessages = typeof zh;
export const productMessages: Record<Locale, ProductMessages> = {
  zh,
  en: {
    app: appMessages.en,
    common: commonMessages.en,
    assistantActionDialog: assistantActionDialogMessages.en,
    assistantChat: assistantChatMessages.en,
    assistantPanels: assistantPanelsMessages.en,
    assistantSidebar: assistantSidebarMessages.en,
    assistantWorkspace: assistantWorkspaceMessages.en,
    chatMessageView: chatMessageViewMessages.en,
    chatShowcase: chatShowcaseMessages.en,
    formerAssistantsPanel: formerAssistantsPanelMessages.en,
    interfaceShowcase: interfaceShowcaseMessages.en,
    interfaceTerminal: interfaceTerminalMessages.en,
    interfaceWorkspace: interfaceWorkspaceMessages.en,
    messageComposer: messageComposerMessages.en,
    scheduleMonth: scheduleMonthMessages.en,
    schedulePanel: schedulePanelMessages.en,
    settingsPanel: settingsPanelMessages.en,
    taskDrawer: taskDrawerMessages.en,
    todoPanel: todoPanelMessages.en,
    workspaceSearchPanel: workspaceSearchPanelMessages.en,
    interfaceDemo: interfaceDemoMessages.en,
    interfacePlatform: interfacePlatformMessages.en,
    logoConnectionShowcase: logoConnectionShowcaseMessages.en,
  },
};
