import { productMessages } from './i18n';
import { useEffect, useState } from 'react';
import { initialSchedules } from './schedule-demo';
import type { DemoSchedule } from './schedule-demo';
import { initialTodos, todoUrgencies } from './todo-demo';
import type { DemoTodo } from './todo-demo';

export type WorkStatus = 'running' | 'paused' | 'blocked' | 'budget' | 'verifying' | 'failed' | 'complete' | 'cancelled';
export type ChatAttachment = { id: string; name: string; size: number; type: string; previewUrl?: string };
export type InterfacePreferences = { showMetrics: boolean; showAvatar: boolean; showSender: boolean; showHints: boolean; showFeedback: boolean };
const defaultInterfacePreferences: InterfacePreferences = { showMetrics: true, showAvatar: true, showSender: true, showHints: false, showFeedback: true };
export type ChatMessage = { images?: { src: string; alt: string }[]; attachments?: ChatAttachment[]; model?: string; feedback?: 'up' | 'down'; readStatus?: 'read' | 'unread'; replyTo?: string; id: string; role: 'user' | 'assistant'; text: string; textEn?: string; taskId?: string };
export type DemoAssistant = { readMessageCount?: number; pendingReply?: ChatMessage; pendingReplies?: ChatMessage[]; draftModel?: string; draftAttachments?: ChatAttachment[]; id: string; name: string; grants: Record<string, boolean>; messages: ChatMessage[]; draft: string; reference: string | null };
export type DemoWork = {
  assistantId: string; revisions: string[]; versions: string[];
  id: string; title: string; titleEn: string; resource: string; status: WorkStatus; done: number; total: number;
  budget: number; spent: number; calls: number; maxCalls: number; stage: number;
  logs: string[]; artifact: string | null; verified: boolean; notification: 'pending' | 'delivered';
};
export type DemoMemory = { kind?: 'memory' | 'preference'; id: string; text: string; source: string; updated: string };
export const resourceCategories = ['Storage', 'Compute', 'Network', 'Tools'] as const;
export type ResourceCategory = typeof resourceCategories[number];
export type DemoResource = { id: string; category: ResourceCategory; zh: string; en: string; path: string; device: string; count: number; files: string[]; scopeZh?: string; scopeEn?: string };
export const demoResources: DemoResource[] = [
  { id: 'meetings', category: 'Storage', zh: '旅行笔记', en: 'Travel notes', path: '~/Documents/Travel', device: 'This device', count: 3, files: ['itinerary.md', 'transport.md', 'packing.md'] },
  { id: 'documents', category: 'Storage', zh: '参考资料', en: 'Reference documents', path: '~/Documents/Reference', device: 'This device', count: 3, files: ['brief.md', 'research.md', 'decisions.md'] },
  { id: 'archive', category: 'Storage', zh: '归档资料', en: 'Archive', path: '~/Documents/Archive', device: 'This device', count: 3, files: ['week-35.md', 'week-36.md', 'week-37.md'] },
  { id: 'local-compute', category: 'Compute', zh: '本机计算', en: 'Local compute', path: 'local://compute', device: 'This device', count: 0, files: [], scopeZh: '受控处理任务 · 并发 1 · 单次最长 5 分钟', scopeEn: 'Controlled processing · Concurrency 1 · Up to 5 minutes per run' },
  { id: 'web-access', category: 'Network', zh: '网页访问', en: 'Web access', path: 'HTTPS · public web', device: 'Network', count: 0, files: [], scopeZh: '读取公开 HTTPS 网页 · 不访问本机和内网地址', scopeEn: 'Read public HTTPS pages · Local and private addresses excluded' },
  { id: 'document-reader', category: 'Tools', zh: '文档读取工具', en: 'Document reader', path: 'tool://document-reader', device: 'Built-in', count: 0, files: [], scopeZh: '解析文本与 Markdown · 仅访问已授权的存储资源', scopeEn: 'Parse text and Markdown · Requires separately authorized storage' },
  { id: 'digest-exporter', category: 'Tools', zh: '摘要导出工具', en: 'Digest exporter', path: 'tool://digest-exporter', device: 'Built-in', count: 0, files: [], scopeZh: '导出 Markdown · 仅写入任务输出区', scopeEn: 'Export Markdown · Write only to task outputs' },
];
export const sourceContent: Record<string, string> = {
  'itinerary.md': '# 行程 / Itinerary\n计划：周六上午出发，周日下午返回。 / Plan: leave Saturday morning, return Sunday afternoon.\n偏好：安排步行游览，保留休息时间。 / Preference: walking tours with rest breaks.',
  'transport.md': '# 交通 / Transport\n待办：确认往返车次与住宿地址。 / Action: confirm return transport and accommodation address.\n状态：尚未预订。 / Status: not booked.',
  'packing.md': '# 出行准备 / Packing\n待办：查看天气，携带雨具与充电器。 / Action: check weather; bring rain gear and a charger.\n未解决：周日返程时间。 / Open question: Sunday return time.',
  'brief.md': '# Brief\n目标：交付可核对的资料摘要。 / Goal: deliver a verifiable document summary.',
  'research.md': '# Research\n待办：核对每条结论的来源。 / Action: check sources for each conclusion.',
  'decisions.md': '# Decisions\n决定：不覆盖原文件。 / Decision: do not overwrite originals.',
  'week-35.md': '# Week 35\n资料归档完成。 / Archive completed.',
  'week-36.md': '# Week 36\n待办：整理后续问题。 / Action: organize open questions.',
  'week-37.md': '# Week 37\n未解决：下一阶段范围。 / Open question: next phase scope.',
};
const report = (resource: string) => {
  const files = demoResources.find(item => item.id === resource)!.files;
  return '# 资料摘要 / Travel and document digest\n\n交互原型生成的示例产物；未调用真实模型。 / Prototype artifact; no model was called.\n\n' + files.map(file => `## 来源 / Source: ${file}\n\n${sourceContent[file]}\n`).join('\n') + '\n## 验收 / Verification\n3 / 3 来源已引用；原始资料未修改。 / All three sources referenced; originals unchanged.\n';
};
const makeWork = (id: string, title: string, titleEn: string, resource: string, budget = 2): DemoWork => ({ assistantId: 'personal', revisions: [], versions: [], id, title, titleEn, resource, budget, status: 'running', done: 0, total: 4, spent: 0, calls: 0, maxCalls: 6, stage: 0, logs: ['accepted'], artifact: null, verified: false, notification: 'pending' });
const initial = () => ({
  todos: initialTodos(),
  interfacePreferences: defaultInterfacePreferences,
  schedules: initialSchedules(),
  activeAssistantId: 'personal',
  assistants: [{ id: 'personal', name: 'Molr', grants: { meetings: true, documents: true, archive: false }, draft: '', reference: null, messages: [{ id: 'welcome', role: 'assistant', text: '有什么想交给我，或者想一起聊聊？你可以一直在这里和我沟通。', textEn: 'What would you like to do or talk about? This is our ongoing conversation.' }, { id: 'delivery', role: 'assistant', text: '上次的出行备忘已整理好。想调整内容，直接回复我。', textEn: 'Your trip digest is ready. Reply here if you want anything changed.', taskId: '023' }] }, { id: 'travel', name: '旅行助理', grants: { meetings: false, documents: false, archive: false }, draft: '', reference: null, messages: [{ id: 'welcome', role: 'assistant', text: '你好，我是旅行助理。先聊聊你的计划；需要资料时，我会申请授权。', textEn: 'Hello! Tell me about your trip. I will request access when I need your documents.' }] }] as DemoAssistant[],
  works: [
    { ...makeWork('024', '整理这次出行资料', 'Prepare my trip brief', 'meetings'), stage: 1, done: 1, spent: .2, calls: 1, logs: ['accepted', 'read'] },
    { ...makeWork('025', '汇总参考资料', 'Summarize reference documents', 'documents', .2), stage: 1, done: 1, spent: .2, calls: 1, status: 'budget' as const, logs: ['accepted', 'read', 'budget'] },
    { ...makeWork('023', '整理上次出行备忘', 'Review my last trip', 'meetings'), stage: 4, done: 4, spent: .6, calls: 3, status: 'complete' as const, artifact: report('meetings'), verified: true, notification: 'delivered' as const, logs: ['accepted', 'read', 'draft', 'saved', 'verified', 'notified'] },
  ],
  grants: { meetings: true, documents: true, archive: false } as Record<string, boolean>,
  connected: true, defaultBudget: 2, defaultMaxCalls: 6,
  memories: [{ id: 'm1', text: '结果附上来源，保留原始文件。 / Include sources and preserve originals.', source: 'user', updated: '2026-09-22' }] as DemoMemory[],
});
type DemoState = ReturnType<typeof initial>;
const storageKey = 'molr-assistant-prototype-v1';
function replyTarget(message: ChatMessage) { return message.replyTo ?? (message.id.startsWith('a-') ? `u-${message.id.slice(2)}` : undefined); }
function restoreReadStatuses(assistant: DemoAssistant): DemoAssistant {
  const answered = new Set(assistant.messages.filter(message => message.role === 'assistant').map(replyTarget));
  return { ...assistant, messages: assistant.messages.map(message => message.role === 'user' ? { ...message, readStatus: message.readStatus ?? (answered.has(message.id) ? 'read' : 'unread') } : message) };
}
function readState(): DemoState {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) ?? 'null');
    if (saved && Array.isArray(saved.works) && saved.works.length && saved.works.every((w: DemoWork) => typeof w.id === 'string' && demoResources.some(r => r.id === w.resource) && Number.isFinite(w.budget) && Array.isArray(w.logs) && w.status in statusLabels.en) && Array.isArray(saved.memories) && saved.grants && typeof saved.connected === 'boolean' && Number.isFinite(saved.defaultBudget) && Number.isFinite(saved.defaultMaxCalls)) { const base = initial(); return { ...saved, todos: Array.isArray(saved.todos) ? saved.todos.map((todo: DemoTodo) => ({ ...todo, urgency: todoUrgencies.includes(todo.urgency) ? todo.urgency : 'normal' })) : initialTodos(), interfacePreferences: { ...defaultInterfacePreferences, ...saved.interfacePreferences }, schedules: Array.isArray(saved.schedules) ? saved.schedules : initialSchedules(), activeAssistantId: saved.activeAssistantId ?? 'personal', assistants: (saved.assistants ?? base.assistants.map(a => a.id === 'personal' ? { ...a, grants: saved.grants } : a)).map(restoreReadStatuses), works: saved.works.map((w: DemoWork) => ({ ...w, assistantId: w.assistantId ?? 'personal', revisions: w.revisions ?? [], versions: w.versions ?? [] })) }; }
  } catch { /* Storage can be unavailable; the prototype still runs in memory. */ }
  return initial();
}
export const statusLabels = {
  zh: { running: '执行中', paused: '已暂停', blocked: '等待授权', budget: '等待预算', verifying: '验收中', failed: '待核对', complete: '已完成', cancelled: '已取消' },
  en: { running: 'In progress', paused: 'Paused', blocked: 'Awaiting access', budget: 'Awaiting budget', verifying: 'Verifying', failed: 'Needs review', complete: 'Completed', cancelled: 'Cancelled' },
};
export const eventLabels = {
  zh: { revision: '对话追加了要求；旧版产物保留，新版本待验收', accepted: '已保存目标、资源范围与预算', read: '已读取 3 份示例资料，保存读取检查点', draft: '已整理内容与来源，保存草稿检查点', saved: '产物已写入任务专属输出区', verified: '已验证产物存在、来源齐全、原件保留', budget: '下一步将超出预算或调用上限，已阻止', revoked: '资源授权已撤销，后续操作已阻止', paused: '已暂停，保留已完成步骤', resumed: '已继续，将从检查点执行', cancelled: '已取消后续执行，已有产物保留', missing: '验收失败：产物缺失，不能标记完成', recovered: '已核对检查点，从未完成的步骤恢复', notified: '结果通知已送达', notificationFailed: '结果已完成，但通知未送达', budgetChanged: '用户调整了本任务的预算' },
  en: { revision: 'Revision received in conversation; previous output preserved', accepted: 'Goal, resource scope, and budget saved', read: 'Read 3 sample sources; checkpoint saved', draft: 'Content and citations drafted; checkpoint saved', saved: 'Artifact saved in this work’s output area', verified: 'Artifact, citations, and preserved originals verified', budget: 'Next step exceeds budget or call limit; blocked', revoked: 'Access revoked; further operations blocked', paused: 'Paused; completed steps preserved', resumed: 'Resumed from checkpoint', cancelled: 'Future execution cancelled; existing artifacts preserved', missing: 'Verification failed: missing artifact; cannot complete', recovered: 'Checkpoint checked; resume from unfinished step', notified: 'Result notification delivered', notificationFailed: 'Work completed, but notification not delivered', budgetChanged: 'User adjusted this work’s budget' },
};
export function useInterfaceDemo() {
  const [state, setState] = useState<DemoState>(readState);
  const [storageAvailable, setStorageAvailable] = useState(true);
  useEffect(() => { try { localStorage.setItem(storageKey, JSON.stringify(state)); setStorageAvailable(true); } catch { setStorageAvailable(false); } }, [state]);
  const pendingIds = state.assistants.map(a => a.pendingReply?.id ?? '').join('|');
  useEffect(() => {
    const pending = state.assistants.flatMap(a => a.pendingReply ? [{ assistantId: a.id, message: a.pendingReply }] : []);
    if (!pending.length) return;
    const timer = setTimeout(() => setState(current => ({ ...current, assistants: current.assistants.map(a => {
      const reply = pending.find(item => item.assistantId === a.id && item.message.id === a.pendingReply?.id);
      return reply ? { ...a, pendingReply: a.pendingReplies?.[0], pendingReplies: a.pendingReplies?.slice(1) ?? [], messages: [...a.messages.map(message => message.role === 'user' && message.id === replyTarget(reply.message) ? { ...message, readStatus: 'read' as const } : message), reply.message] } : a;
    }) })), 650);
    return () => clearTimeout(timer);
  // Only a new pending reply restarts the simulated typing interval.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pendingIds]);
  const unreadCounts = Object.fromEntries(state.assistants.map(a => [a.id, Math.max(0, a.messages.filter(m => m.role === 'assistant').length - (a.readMessageCount ?? 0))]));
  function markAssistantRead(id: string) { setState(current => {
    const assistant = current.assistants.find(a => a.id === id); if (!assistant) return current;
    const count = assistant.messages.filter(m => m.role === 'assistant').length;
    if (assistant.readMessageCount === count) return current;
    return { ...current, assistants: current.assistants.map(a => a.id === id ? { ...a, readMessageCount: count } : a) };
  }); }
  function resetUnread() { setState(current => ({ ...current, assistants: current.assistants.map(assistant => ({ ...assistant, readMessageCount: 0 })) })); }
  const activeAssistant = state.assistants.find(a => a.id === state.activeAssistantId)!;
  function updateWork(id: string, apply: (work: DemoWork, current: DemoState) => DemoWork) {
    setState(current => {
      const previous = current.works.find(w => w.id === id && w.assistantId === current.activeAssistantId);
      if (!previous) return current;
      const next = apply(previous, current);
      const delivered = next.status === 'complete' && next.notification === 'delivered' && (previous.status !== 'complete' || previous.notification !== 'delivered');
      const message: ChatMessage = { id: `result-${next.id}-${next.revisions.length}`, role: 'assistant', text: '任务结果已完成并通过验收，你可以在任务中查看交付物，或继续告诉我需要修改的地方。', textEn: 'The result is complete and verified. View the deliverable in Tasks, or tell me what you would like changed.', taskId: next.id };
      return { ...current, works: current.works.map(w => w.id === id ? next : w), assistants: delivered ? current.assistants.map(a => a.id === next.assistantId ? { ...a, messages: [...a.messages, message] } : a) : current.assistants };
    });
  }

  function changeGrant(resource: string, allowed: boolean) {
    setState(current => ({ ...current, assistants: current.assistants.map(a => a.id === current.activeAssistantId ? { ...a, grants: { ...a.grants, [resource]: allowed } } : a), works: allowed ? current.works : current.works.map(work => work.assistantId === current.activeAssistantId && work.resource === resource && !['complete', 'cancelled'].includes(work.status) ? { ...work, status: 'blocked', logs: [...work.logs, 'revoked'] } : work) }));
  }
  function toggleWork(id: string) {
    const work = state.works.find(item => item.id === id);
    if (!work || ['complete', 'cancelled', 'failed'].includes(work.status) || !state.connected || !activeAssistant.grants[work.resource]) return false;
    updateWork(id, (item, current) => {
      if (!current.connected || !current.assistants.find(a => a.id === item.assistantId)!.grants[item.resource]) return item;
      if (['running', 'verifying'].includes(item.status)) return { ...item, status: 'paused', logs: [...item.logs, 'paused'] };
      if (item.stage < 3 && (item.spent + .2 > item.budget + .001 || item.calls >= item.maxCalls)) return { ...item, status: 'budget' };
      return { ...item, status: item.stage === 3 ? 'verifying' : 'running', logs: [...item.logs, 'resumed'] };
    });
    return true;
  }
  function createWork(title: string, resource: string, budget = state.defaultBudget) {
    if (!title.trim() || !demoResources.some(item => item.id === resource && item.category === 'Storage') || !activeAssistant.grants[resource] || !state.connected || !Number.isFinite(budget) || budget < .2 || budget > 100) return null;
    const id = String(Math.max(...state.works.map(work => Number(work.id))) + 1).padStart(3, '0');
    setState(current => ({ ...current, works: [...current.works, { ...makeWork(id, title.trim(), title.trim(), resource, budget), maxCalls: current.defaultMaxCalls, assistantId: current.activeAssistantId }] }));
    return id;
  }
  function advanceWork(id: string, fault?: 'missing' | 'notification') {
    updateWork(id, (work, current) => {
      if (!current.connected || !['running', 'verifying'].includes(work.status)) return work;
      if (!current.assistants.find(a => a.id === work.assistantId)!.grants[work.resource]) return { ...work, status: 'blocked', logs: [...work.logs, 'revoked'] };
      if (work.stage < 3) {
        if (work.spent + .2 > work.budget + .001 || work.calls >= work.maxCalls) return { ...work, status: 'budget', logs: [...work.logs, 'budget'] };
        const stage = work.stage + 1;
        return { ...work, stage, done: stage, spent: Math.round((work.spent + .2) * 100) / 100, calls: work.calls + 1, artifact: stage === 3 ? report(work.resource) + (work.revisions.length ? '\n## 追加要求 / Requested revisions\n' + work.revisions.join('\n') : '') : work.artifact, status: stage === 3 ? 'verifying' : 'running', logs: [...work.logs, ['read', 'draft', 'saved'][work.stage]] };
      }
      if (fault === 'missing' || !work.artifact) return { ...work, artifact: null, verified: false, status: 'failed', logs: [...work.logs, 'missing'] };
      const citationsValid = demoResources.find(item => item.id === work.resource)!.files.every(file => work.artifact!.includes(file));
      if (!citationsValid) return { ...work, status: 'failed', logs: [...work.logs, 'missing'] };
      return { ...work, stage: 4, done: 4, verified: true, status: 'complete', notification: fault === 'notification' ? 'pending' : 'delivered', logs: [...work.logs, 'verified', fault === 'notification' ? 'notificationFailed' : 'notified'] };
    });
  }
  function recoverWork(id: string) { updateWork(id, (work, current) => work.status !== 'failed' || !current.connected ? work : ({ ...work, stage: 2, done: 2, status: current.assistants.find(a => a.id === work.assistantId)!.grants[work.resource] ? 'paused' : 'blocked', logs: [...work.logs, 'recovered'] })); }
  function cancelWork(id: string) { updateWork(id, work => ['complete', 'cancelled'].includes(work.status) ? work : { ...work, status: 'cancelled', logs: [...work.logs, 'cancelled'] }); }
  function setBudget(id: string, budget: number) { if (Number.isFinite(budget) && budget >= .2 && budget <= 100) updateWork(id, work => ({ ...work, budget, logs: [...work.logs, 'budgetChanged'] })); }
  function retryNotification(id: string) { updateWork(id, (work, current) => current.connected && work.status === 'complete' && work.notification === 'pending' ? { ...work, notification: 'delivered', logs: [...work.logs, 'notified'] } : work); }
  function saveMemory(text: string, id?: string, kind: 'memory' | 'preference' = 'preference') { if (!text.trim()) return; setState(current => ({ ...current, memories: id ? current.memories.map(memory => memory.id === id ? { ...memory, text: text.trim(), updated: new Date().toISOString().slice(0, 10) } : memory) : [...current.memories, { id: `m${Math.max(0, ...current.memories.map(memory => Number(memory.id.slice(1)) || 0)) + 1}`, kind, text: text.trim(), source: 'user', updated: new Date().toISOString().slice(0, 10) }] })); }
  function patchAssistant(patch: Partial<DemoAssistant>) { setState(current => ({ ...current, assistants: current.assistants.map(a => a.id === current.activeAssistantId ? { ...a, ...patch } : a) })); }
  function addAssistant(name: string) {
    if (!name.trim()) return;
    setState(current => { const id = `assistant-${Date.now()}-${current.assistants.length}`; return { ...current, activeAssistantId: id, assistants: [...current.assistants, { id, name: name.trim(), grants: { meetings: false, documents: false, archive: false }, messages: [], draft: '', reference: null }] }; });
  }
  function sendMessage(text: string, taskId?: string, locale: 'zh' | 'en' = 'zh') {
    if ((!text.trim() && !activeAssistant.draftAttachments?.length) || !state.connected) return;
    setState(current => {
      const assistant = current.assistants.find(a => a.id === current.activeAssistantId)!;
      const target = taskId ? current.works.find(w => w.id === taskId && w.assistantId === assistant.id) : undefined;
      let works = current.works;
      let relatedId = target?.id;
      let reply = productMessages[locale].interfaceDemo.iHearYouKeepTalkingNotEveryMessageNeeds;
      if (target && !/(为什么|怎么|了吗|进度|状态|\?|？|\bwhy\b|\bstatus\b|\bhow\b)/i.test(text)) {
        works = works.map(w => w.id !== target.id ? w : { ...w, revisions: [...w.revisions, text.trim()], versions: w.artifact ? [...w.versions, w.artifact] : w.versions, artifact: null, verified: false, notification: 'pending', stage: 0, done: 0, status: !assistant.grants[w.resource] ? 'blocked' : w.spent + .2 > w.budget + .001 || w.calls >= w.maxCalls ? 'budget' : 'running', logs: [...w.logs, 'revision'] });
        reply = productMessages[locale].interfaceDemo.yourRevisionIsAttachedToTheSameTaskPrevious;
      } else if (target) {
        reply = productMessages[locale].interfaceDemo.thisTaskIs4StepsCompleteReviewTheDeliverable(statusLabels[locale][target.status], target.stage);
      } else if (/(整理|生成|准备|汇总|帮我|制定|prepare|organize|summarize|create|make)/i.test(text)) {
        relatedId = String(Math.max(0, ...works.map(w => Number(w.id))) + 1).padStart(3, '0');
        const created = { ...makeWork(relatedId, text.trim(), text.trim(), 'meetings', current.defaultBudget), assistantId: assistant.id, maxCalls: current.defaultMaxCalls };
        if (!assistant.grants.meetings) created.status = 'blocked';
        works = [...works, created];
        reply = productMessages[locale].interfaceDemo.iHaveRecordedThisAsATaskTheDemo;
      }
      const suffix = `${Date.now()}-${assistant.messages.length}`;
      const messages: ChatMessage[] = [...assistant.messages, { id: `u-${suffix}`, role: 'user', readStatus: 'unread', text: text.trim(), attachments: assistant.draftAttachments ?? [], taskId: target?.id }];
      const pendingReply: ChatMessage = { id: `a-${suffix}`, role: 'assistant', replyTo: `u-${suffix}`, text: reply, model: assistant.draftModel ?? 'standard', taskId: relatedId };
      return { ...current, works, assistants: current.assistants.map(a => a.id === assistant.id ? { ...a, messages, pendingReply: a.pendingReply ?? pendingReply, pendingReplies: a.pendingReply ? [...(a.pendingReplies ?? []), pendingReply] : [], draft: '', draftAttachments: [], reference: relatedId ?? null } : a) };
    });
  }
  function saveSchedule(schedule: DemoSchedule) {
    if (!schedule.title.trim() || !/^\d{4}-\d{2}-\d{2}$/.test(schedule.date) || !/^([01]\d|2[0-3]):[0-5]\d$/.test(schedule.time) || (schedule.kind === 'person' && (!/^([01]\d|2[0-3]):[0-5]\d$/.test(schedule.endTime) || schedule.endTime <= schedule.time)) || (schedule.kind === 'assistant' && !state.assistants.some(a => a.id === schedule.assistantId))) return false;
    setState(current => ({ ...current, schedules: schedule.id ? current.schedules.map(s => s.id === schedule.id ? { ...schedule, title: schedule.title.trim() } : s) : [...current.schedules, { ...schedule, title: schedule.title.trim(), id: `s-${Date.now()}-${current.schedules.length}` }] }));
    return true;
  }
  function saveTodo(todo: DemoTodo) {
    if ((todo.start || todo.end) && (!todo.start || !todo.end || todo.end <= todo.start)) return false;
    if (!todoUrgencies.includes(todo.urgency) || !todo.title.trim() || (todo.owner === 'assistant' && !state.assistants.some(a => a.id === todo.assistantId))) return false;
    setState(current => ({ ...current, todos: todo.id ? current.todos.map(item => item.id === todo.id ? { ...todo, title: todo.title.trim() } : item) : [...current.todos, { ...todo, title: todo.title.trim(), id: `todo-${Date.now()}-${current.todos.length}` }] }));
    return true;
  }
  return { ...state, unreadCounts, unreadTotal: Object.values(unreadCounts).reduce((sum, n) => sum + n, 0), markAssistantRead, resetUnread, saveTodo,
    toggleTodo: (id: string) => setState(current => ({ ...current, todos: current.todos.map(item => item.id === id ? { ...item, completed: !item.completed } : item) })),
    deleteTodo: (id: string) => setState(current => ({ ...current, todos: current.todos.filter(item => item.id !== id) })),
    setInterfacePreference: (key: keyof InterfacePreferences, value: boolean) => setState(current => ({ ...current, interfacePreferences: { ...current.interfacePreferences, [key]: value } })),
    setMessageFeedback: (id: string, feedback: 'up' | 'down') => setState(current => ({ ...current, assistants: current.assistants.map(a => a.id === current.activeAssistantId ? { ...a, messages: a.messages.map(m => m.id === id ? { ...m, feedback: m.feedback === feedback ? undefined : feedback } : m) } : a) })),
    allWorks: state.works, saveSchedule,
    renameAssistant: (id: string, name: string) => { if (name.trim()) setState(current => ({ ...current, assistants: current.assistants.map(a => a.id === id ? { ...a, name: name.trim() } : a) })); },
    deleteSchedule: (id: string) => setState(current => ({ ...current, schedules: current.schedules.filter(s => s.id !== id) })),
    toggleSchedule: (id: string) => setState(current => ({ ...current, schedules: current.schedules.map(s => s.id === id && s.kind === 'assistant' ? { ...s, enabled: !s.enabled } : s) })),
    works: state.works.filter(w => w.assistantId === state.activeAssistantId), grants: activeAssistant.grants, activeAssistant, patchAssistant, addAssistant, sendMessage, selectAssistant: (id: string) => { if (state.assistants.some(a => a.id === id)) setState(current => ({ ...current, activeAssistantId: id })); }, storageAvailable, changeGrant, toggleWork, createWork, advanceWork, recoverWork, cancelWork, setBudget, retryNotification, saveMemory,
    deleteMemory: (id: string) => setState(current => ({ ...current, memories: current.memories.filter(memory => memory.id !== id) })),
    setConnected: (connected: boolean) => setState(current => ({ ...current, connected })),
    setDefaults: (budget: number, calls: number) => { if (Number.isFinite(budget) && budget >= .2 && budget <= 100 && Number.isInteger(calls) && calls >= 1 && calls <= 20) setState(current => ({ ...current, defaultBudget: budget, defaultMaxCalls: calls })); },
    resetDemo: () => setState(initial()),
  };
}
export type InterfaceDemo = ReturnType<typeof useInterfaceDemo>;
