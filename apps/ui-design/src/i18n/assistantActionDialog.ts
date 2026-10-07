// UI copy: keep both locales in sync; parameter types are checked against zh.
const zh = {
  dismiss: (name: string) => `确认解雇「${name}」？`,
  yourConversationWithThisAssistantWillBeArchivedAnd: "解雇后，你与该助理的聊天记录将归档，无法再发送新消息。",
  youCanFindFormerAssistantsViewArchivedConversationsAnd: "你可以在「设置 → 已离职助理」中查询已离职助理、查看归档聊天记录，并返聘助理。",
  confirmDismissal: "确认解雇",
  clone: (name: string) => `确认克隆「${name}」？`,
  theNewAssistantWillInheritTheOriginalAssistantS: "克隆后的新助理将继承原助理的记忆、消息记录和权限。",
  confirmThatThisContextAndAccessShouldBeShared: "请确认这些内容和访问权限都适合交给新助理。",
  confirmClone: "确认克隆",
  rehireAssistant: "返聘助理",
  rehire: (name: string) => `确认返聘「${name}」？`,
  thisAssistantWillReturnToYourAssistantListAnd: "返聘后，该助理将重新出现在助理列表中，你可以继续发送新消息。",
  yourPreviousMessagesWillRemainAvailableSoYouCan: "原有聊天记录会保留，方便继续之前的对话。",
  confirmRehire: "确认返聘",
  interactionPreview: "交互示意",
  thisPreviewsTheInteractionNoAssistantsOrConversationsWere: "此操作仅作演示，未修改助理或聊天记录。",
};

const en: typeof zh = {
  dismiss: (name) => `Dismiss “${name}”?`,
  yourConversationWithThisAssistantWillBeArchivedAnd: "Your conversation with this assistant will be archived, and you will no longer be able to send new messages.",
  youCanFindFormerAssistantsViewArchivedConversationsAnd: "You can find former assistants, view archived conversations, and rehire them in Settings → Former assistants.",
  confirmDismissal: "Confirm dismissal",
  clone: (name) => `Clone “${name}”?`,
  theNewAssistantWillInheritTheOriginalAssistantS: "The new assistant will inherit the original assistant’s memories, message history, and permissions.",
  confirmThatThisContextAndAccessShouldBeShared: "Confirm that this context and access should be shared with the new assistant.",
  confirmClone: "Confirm clone",
  rehireAssistant: "Rehire assistant",
  rehire: (name) => `Rehire “${name}”?`,
  thisAssistantWillReturnToYourAssistantListAnd: "This assistant will return to your assistant list, and you will be able to send new messages again.",
  yourPreviousMessagesWillRemainAvailableSoYouCan: "Your previous messages will remain available so you can continue the conversation.",
  confirmRehire: "Confirm rehire",
  interactionPreview: "Interaction preview",
  thisPreviewsTheInteractionNoAssistantsOrConversationsWere: "This previews the interaction. No assistants or conversations were changed.",
};

export const assistantActionDialogMessages = { zh, en };
