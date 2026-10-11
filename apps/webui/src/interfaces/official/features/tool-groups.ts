import { Brain, CalendarClock, Folder, MessagesSquare, Wrench } from 'lucide-react';

export const toolGroups = [
  { key: 'conversation', icon: MessagesSquare },
  { key: 'files', icon: Folder },
  { key: 'plans', icon: CalendarClock },
  { key: 'memory', icon: Brain },
  { key: 'execution', icon: Wrench },
] as const;

export type ToolGroup = typeof toolGroups[number]['key'];

export function toolGroup(name: string): ToolGroup {
  if (name.startsWith('conversation_') || name.startsWith('profile_')) return 'conversation';
  if (name.startsWith('file_')) return 'files';
  if (name.startsWith('plan_')) return 'plans';
  if (name.startsWith('memory_')) return 'memory';
  return 'execution';
}
