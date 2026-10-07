// UI copy: keep both locales in sync; parameter types are checked against zh.
const zh = {
  iHearYouKeepTalkingNotEveryMessageNeeds: "我记下了。你可以继续聊，不需要为每条消息创建任务。（当前为模拟对话，尚未连接模型。）",
  yourRevisionIsAttachedToTheSameTaskPrevious: "已把追加要求记入原任务，旧版交付物会保留。新的结果还需执行与验收；权限和预算仍按原有边界检查。",
  thisTaskIs4StepsCompleteReviewTheDeliverable: (status: string, stage: number) => `这项任务目前${status}，已完成 ${stage}/4 个步骤。你可以查看交付物，也可以继续提出修改要求。（当前为模拟回复。）`,
  iHaveRecordedThisAsATaskTheDemo: "这件事我已记为任务。演示会读取旅行资料并生成摘要，原件保持不变；如果需要调整，继续在这里告诉我。",
};

const en: typeof zh = {
  iHearYouKeepTalkingNotEveryMessageNeeds: "I hear you. Keep talking; not every message needs a task. (Simulated conversation; no model connected.)",
  yourRevisionIsAttachedToTheSameTaskPrevious: "Your revision is attached to the same task. Previous deliverables are preserved. The new result needs execution and verification within existing access and budget.",
  thisTaskIs4StepsCompleteReviewTheDeliverable: (status, stage) => `This task is ${status}, ${stage}/4 steps complete. Review the deliverable or ask for changes. (Simulated reply.)`,
  iHaveRecordedThisAsATaskTheDemo: "I have recorded this as a task. The demo reads travel notes and produces a digest while preserving originals. Tell me here if anything should change.",
};

export const interfaceDemoMessages = { zh, en };
