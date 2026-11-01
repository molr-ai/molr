import { useCallback } from 'react';
import { Badge, IconButton } from '@molr/ui';
import { Globe, ListTodo, Terminal } from 'lucide-react';
import type { BrowserStatus, ShellTask } from '@molr/sdk';
import { useCopy, useQuery, useQueryValue } from '../../../state';

const browserTabs = (data?: BrowserStatus) => data?.Task ? data.TabCount : undefined;
const terminalCount = (data?: { Items: ShellTask[] }) => data?.Items.filter(task => task.Mode === 'tty' && task.State === 'running').length ?? 0;
const runningCount = (data?: { Items: ShellTask[] }) => data?.Items.filter(task => task.State === 'running').length ?? 0;

export function BrowserButton({ assistantID, disabled, onOpen }: { assistantID: string; disabled?: boolean; onOpen: () => void }) {
  const { locale } = useCopy();
  const cadence = useCallback((data?: BrowserStatus) => data?.Task ? 2000 : false, []);
  const count = useQueryValue(disabled ? null : `/api/assistants/${assistantID}/browser`, browserTabs, false, cadence);
  return <IconButton className="chat-count-trigger" disabled={disabled} onClick={onOpen} label={`${locale === 'zh' ? '浏览器' : 'Browser'}${count ? ` (${count})` : ''}`}><Globe size={20}/>{!!count && <Badge tone="positive" size="sm" dot={false} aria-hidden="true">{count}</Badge>}</IconButton>;
}

export function TerminalButton({ assistantID, disabled, onOpen }: { assistantID: string; disabled?: boolean; onOpen: () => void }) {
  const { locale } = useCopy();
  const count = useQueryValue(disabled ? null : `/api/assistants/${assistantID}/shell-tasks`, terminalCount, false, 0);
  return <IconButton className="chat-count-trigger" disabled={disabled} onClick={onOpen} label={`${locale === 'zh' ? '终端' : 'Terminal'}${count ? ` (${count})` : ''}`}><Terminal size={20}/>{count > 0 && <Badge tone="positive" size="sm" dot={false} aria-hidden="true">{count}</Badge>}</IconButton>;
}


export function useShellTaskList(assistantID: string | null) {
  const path = assistantID ? `/api/assistants/${assistantID}/shell-tasks` : null;
  const query = useQuery<{ Items: ShellTask[] }>(path, false, 0);
  return { items: query.data?.Items ?? [], error: !!query.error, loading: query.loading && !query.data, refresh: query.reload };
}

export function ShellTasksButton({ assistantID, disabled, onOpen }: { assistantID: string; disabled?: boolean; onOpen: () => void }) {
  const { locale } = useCopy();
  const running = useQueryValue(disabled ? null : `/api/assistants/${assistantID}/shell-tasks`, runningCount, false, 0);
  return <IconButton className="chat-count-trigger" disabled={disabled} onClick={onOpen} label={`${locale === 'zh' ? '运行任务' : 'Running tasks'}${running > 0 ? ` (${running})` : ''}`}><ListTodo size={20} />{running > 0 && <Badge tone="positive" size="sm" dot={false} aria-hidden="true">{running}</Badge>}</IconButton>;
}
