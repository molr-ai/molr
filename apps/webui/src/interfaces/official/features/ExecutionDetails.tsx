import { useErrorToast } from '../../../error-toasts';
import { useState } from 'react';
import { Badge, Button, Card, Textarea } from '@molr/ui';
import type { Task, Revision, Usage } from '../../../api/types';
import { api } from '../../../api/client';
import { useCopy, useQuery } from '../../../state';
import { Confirm, EditDialog, lines, useAction } from '../components/common';
import { MoneyText } from '../components/Money';
import { Attachment } from './Chat';
import { DreamConflictNotice } from './DreamConflictNotice';
export function ExecutionDetails({ task }: { task: Task }) {
 const { t } = useCopy(), action = useAction(); const [revise,setRevise]=useState(false),[cancel,setCancel]=useState(false);
 useErrorToast(action.error);
  return <section className="plan-execution-details">{task.Plan?.Kind==='schedule' && task.OccurrenceAt && !['completed','failed','canceled'].includes(task.State) && <DreamConflictNotice assistantID={task.AssistantID} startAt={task.OccurrenceAt} />}<Badge>{t.tasks[task.State as 'pending'] ?? task.State}</Badge><p>{task.WaitingReason}</p><TaskSpend task={task} />
 <div className="form-actions">{!['completed','failed','canceled'].includes(task.State) && <><Button size="sm" disabled={action.busy} onClick={() => void action.run(() => api.mutate(`/api/tasks/${task.State === 'paused' ? 'resume' : 'pause'}`, { ID: task.ID }, task.Version))}>{task.State === 'paused' ? t.tasks.resume : t.tasks.pause}</Button><Button size="sm" variant="ghost" onClick={() => setCancel(true)}>{t.tasks.cancel}</Button></>}<Button size="sm" variant="secondary" disabled={task.State === 'running'} onClick={() => setRevise(true)}>{t.tasks.revise}</Button></div>
 {task.ArtifactIDs?.map(id => <Attachment key={id} id={id} />)}<Revisions taskID={task.ID} />
 {revise && <RevisionEditor task={task} onClose={() => setRevise(false)} />}{cancel && <Confirm title={t.tasks.cancel} description={t.tasks.cancelHelp} danger onClose={() => setCancel(false)} onConfirm={() => api.mutate('/api/tasks/cancel', { ID: task.ID }, task.Version)} />}
 </section>;
}
function Revisions({ taskID }: { taskID: string }) {
  const { t } = useCopy(), query = useQuery<Revision[]>(`/api/tasks/${taskID}/revisions`, true);
  useErrorToast(query.error, query.reload);
  return <section><h3>{t.tasks.revisions}</h3>{query.data?.map(row => <Card key={row.Number}><h4>v{row.Number}</h4><p>{row.Instructions}</p><ul>{row.AcceptanceCriteria?.map((text, index) => <li key={index}>{text}</li>)}</ul>{row.ArtifactIDs?.map(id => <Attachment id={id} key={id} />)}</Card>)}</section>;
}
function RevisionEditor({ task, onClose }: { task: Task; onClose: () => void }) {
  const { t } = useCopy(), [instructions, setInstructions] = useState(''), [criteria, setCriteria] = useState('');
  return <EditDialog title={t.tasks.revise} onClose={onClose} onSubmit={() => api.mutate('/api/tasks/revise', { ID: task.ID, Instructions: instructions.trim(), AcceptanceCriteria: lines(criteria) }, task.Version)}><Textarea label={t.tasks.instructions} value={instructions} onChange={event => setInstructions(event.target.value)} required /><Textarea label={t.tasks.criteria} value={criteria} onChange={event => setCriteria(event.target.value)} required /></EditDialog>;
}

function TaskSpend({ task }: { task: Task }) {
  const { t } = useCopy();
  const usage = useQuery<Usage>(`/api/usage?assistant_id=${task.AssistantID}&task_id=${task.ID}`);
  useErrorToast(usage.error, usage.reload);
  return <>{usage.data && <p>{t.settings.spent} · <MoneyText value={usage.data.Spent} originals={usage.data.OriginalSpent} /></p>}</>;
}
