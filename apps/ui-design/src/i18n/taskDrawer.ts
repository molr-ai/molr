// UI copy: keep both locales in sync; parameter types are checked against zh.
const zh = {
  closeTasks: "关闭计划",
  allTasks: "全部计划",
  taskControls: "本次执行",
  cancelTask: "取消任务",
  recoverCheckpoint: "恢复检查点",
  taskBudgetCny: "任务预算（元）",
  updateBudget: "更新额度",
  cancelFutureExecutionAndKeepExistingDeliverables: "取消后续执行，保留已有产物？",
  confirmCancel: "确认取消",
  noTasksYetAskYourAssistantInTheConversation: "暂时没有任务，回到对话交代一件事吧。",
};

const en: typeof zh = {
  closeTasks: "Close plans",
  allTasks: "All plans",
  taskControls: "Execution controls",
  cancelTask: "Cancel task",
  recoverCheckpoint: "Recover checkpoint",
  taskBudgetCny: "Task budget (CNY)",
  updateBudget: "Update budget",
  cancelFutureExecutionAndKeepExistingDeliverables: "Cancel future execution and keep existing deliverables?",
  confirmCancel: "Confirm cancel",
  noTasksYetAskYourAssistantInTheConversation: "No tasks yet. Ask your assistant in the conversation.",
};

export const taskDrawerMessages = { zh, en };
