import { useCallback } from 'react';
import { Badge, IconButton } from '@molr/ui';
import { Globe, ListTodo, Terminal } from 'lucide-react';
import type { BrowserStatus, ShellTask } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';

export function BrowserButton({ assistantID, disabled, onOpen }: { assistantID: string; disabled?: boolean; onOpen: () => void }) {
  const { locale } = useCopy();
  const cadence = useCallback((data?: BrowserStatus) => data?.Task ? 2000 : 5000, []);
  const { data } = useQuery<BrowserStatus>(disabled ? null : `/api/assistants/${assistantID}/browser`, false, cadence);
  const count = data?.Task ? data.TabCount : undefined;
  return <IconButton className="chat-count-trigger" disabled={disabled} onClick={onOpen} label={`${locale === 'zh' ? '浏览器' : 'Browser'}${count ? ` (${count})` : ''}`}><Globe size={20}/>{!!count && <Badge tone="positive" size="sm" dot={false} aria-hidden="true">{count}</Badge>}</IconButton>;
}

export function TerminalButton({ assistantID, disabled, onOpen }: { assistantID: string; disabled?: boolean; onOpen: () => void }) {
  const { locale } = useCopy();
  const cadence = useCallback((data?: { Items: ShellTask[] }) => data?.Items.some(task => task.Mode === 'tty' && task.State === 'running') ? 2000 : 5000, []);
  const { data } = useQuery<{ Items: ShellTask[] }>(disabled ? null : `/api/assistants/${assistantID}/shell-tasks`, false, cadence);
  const count = data?.Items.filter(task => task.Mode === 'tty' && task.State === 'running').length ?? 0;
  return <IconButton className="chat-count-trigger" disabled={disabled} onClick={onOpen} label={`${locale === 'zh' ? '终端' : 'Terminal'}${count ? ` (${count})` : ''}`}><Terminal size={20}/>{count > 0 && <Badge tone="positive" size="sm" dot={false} aria-hidden="true">{count}</Badge>}</IconButton>;
}


export function useShellTaskList(assistantID: string | null, interval: number) {
  const path = assistantID ? `/api/assistants/${assistantID}/shell-tasks` : null;
  const cadence = useCallback((data?: { Items: ShellTask[] }) => data?.Items.some(task => task.State === 'running') ? interval : 30_000, [interval]);
  const query = useQuery<{ Items: ShellTask[] }>(path, false, cadence);
  return { items: query.data?.Items ?? [], error: !!query.error, loading: query.loading && !query.data, refresh: query.reload };
}

export function ShellTasksButton({ assistantID, disabled, onOpen }: { assistantID: string; disabled?: boolean; onOpen: () => void }) {
  const { locale } = useCopy(), { items } = useShellTaskList(disabled ? null : assistantID, 5000);
  const running = items.filter(task => task.State === 'running').length;
  return <IconButton className="chat-count-trigger" disabled={disabled} onClick={onOpen} label={`${locale === 'zh' ? '运行任务' : 'Running tasks'}${running > 0 ? ` (${running})` : ''}`}><ListTodo size={20} />{running > 0 && <Badge tone="positive" size="sm" dot={false} aria-hidden="true">{running}</Badge>}</IconButton>;
}
