import { useErrorToast } from '../../../error-toasts';
import { CalendarDays, Folder, ListTodo } from 'lucide-react';
import { Button, ChatCard } from '@molr/ui';
import type { ChatCardDetail, ChatCardProps } from '@molr/ui';
import type { Message, Resource, Schedule, Task } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';

type Card = NonNullable<Message['Content']['Card']>;
type Props = { card: Card; timeZone?: string; onOpen: (kind: string, id: string) => void };

// Card layout belongs to ChatCard. This adapter supplies current domain data
// while retaining the assistant's original title and summary in the message.
export function ConversationCard({ card, timeZone, onOpen }: Props) {
  const { t, locale } = useCopy();
  const tasks = useQuery<Task>(card.Kind === 'task' ? `/api/tasks/${card.TargetID}` : null);
  const resources = useQuery<Resource>(card.Kind === 'resource' ? `/api/resources/${card.TargetID}` : null);
  const schedules = useQuery<Schedule>(card.Kind === 'schedule' ? `/api/schedules/${card.TargetID}` : null);
  const details: ChatCardDetail[] = [];
  let status: ChatCardProps['status'];
  let title = card.Title;
  const formatDate = (value: string, zone = timeZone) => new Intl.DateTimeFormat(locale, { timeZone: zone || undefined, year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value));
  if (tasks.data) {
    const task = tasks.data;
    title ||= task.PlanTitle || task.Goal;
    status = { label: (t.tasks as Record<string,string>)[task.State] ?? task.State, tone: task.State === 'completed' ? 'positive' : task.State === 'failed' ? 'danger' : task.State === 'waiting' ? 'warning' : 'neutral' };
    details.push({ label: t.tasks.artifacts, value: task.ArtifactIDs?.length ?? 0 });
    if (task.CreatedAt) details.push({ label: t.messageCards.created, value: formatDate(task.CreatedAt) });
  }
  if (resources.data) {
    const resource = resources.data;
    title ||= resource.Name;
    const states = { available: t.resources.available, offline: t.resources.offline, disconnected: t.resources.disconnected };
    status = { label: states[resource.State as keyof typeof states] ?? resource.State, tone: resource.State === 'available' ? 'positive' : 'neutral' };
    details.push({ label: t.messageCards.resource, value: resource.Name }, { label: t.resourcePermissions.resourceType, value: t.resources.categories[resource.Kind] });
  }
  if (schedules.data) {
    const schedule = schedules.data;
    title ||= schedule.Title;
    status = { label: schedule.Enabled ? t.enabled : t.disabled, tone: schedule.Enabled ? 'positive' : 'neutral' };
    details.push({ label: schedule.NextAt ? t.schedules.next : t.schedules.start, value: formatDate(schedule.NextAt || schedule.Trigger.StartAt, schedule.Trigger.Timezone) }, { label: t.schedules.timezone, value: schedule.Trigger.Timezone });
  }
  const kind = card.Kind === 'task' ? t.chat.tasks : card.Kind === 'resource' ? t.nav.resources : t.nav.schedules;
  const action = card.Kind === 'task' ? t.messageCards.viewTask : card.Kind === 'resource' ? t.messageCards.viewResource : t.messageCards.viewSchedule;
  const Icon = card.Kind === 'task' ? ListTodo : card.Kind === 'resource' ? Folder : CalendarDays;
  useErrorToast(tasks.error || resources.error || schedules.error, () => {tasks.reload();resources.reload();schedules.reload();});
  return <ChatCard eyebrow={kind} title={title || kind} icon={<Icon />} description={card.Summary} status={status} details={details}
    actions={<Button variant="secondary" size="sm" onClick={() => onOpen(card.Kind, card.TargetID)}>{action}</Button>}>
  </ChatCard>;
}
