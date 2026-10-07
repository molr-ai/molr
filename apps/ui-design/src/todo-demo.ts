export const todoUrgencies = ['urgent', 'high', 'normal', 'low'] as const;
export type TodoUrgency = typeof todoUrgencies[number];
export type DemoTodo = { urgency: TodoUrgency; id: string; title: string; owner: 'person' | 'assistant'; assistantId: string; due: string; start?: string; end?: string; notes: string; completed: boolean };
export const initialTodos = (): DemoTodo[] => [
  { urgency: 'high', id: 'todo-1', title: '确认返程时间 / Confirm return time', owner: 'person', assistantId: '', due: '', notes: '', completed: false },
  { urgency: 'normal', id: 'todo-2', title: '核对出行清单 / Review travel checklist', owner: 'assistant', assistantId: 'personal', due: '', notes: '等待助理处理 / Awaiting assistant', completed: false },
];
